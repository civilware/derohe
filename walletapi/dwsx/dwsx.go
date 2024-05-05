package dwsx

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"unicode"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/walletapi"
	"github.com/deroproject/derohe/walletapi/rpcserver"
	"github.com/go-logr/logr"
)

type messageRequest struct {
	app     *ApplicationData
	conn    *Connection
	request *jrpc2.Request
}

type messageRegistration struct {
	app     *ApplicationData
	conn    *Connection
	request *http.Request
}

type DWSX struct {
	// The websocket connected to and its app data
	applications map[*Connection]ApplicationData
	// function to request access of a dApp to wallet
	appHandler func(*ApplicationData) bool
	// function to request the permission
	requestHandler func(*ApplicationData, *jrpc2.Request) Permission
	handlerMutex   sync.Mutex
	server         *http.Server
	logger         logr.Logger
	context        *rpcserver.WalletContext
	wallet         *walletapi.Wallet_Disk
	rpcHandler     handler.Map
	running        bool
	forceAsk       bool // forceAsk ensures no permissions can be accepted upon initial connection
	requests       chan messageRequest
	registers      chan messageRegistration
	// context and cancel to cleanly exit handler_loop
	ctx    context.Context
	cancel context.CancelFunc
	// mutex for applications map
	sync.Mutex
}

// This is default port for DWSX
// It can be changed for tests only
// Production should always use 44326 as its a way to identify DWSX
const DWSX_PORT = 44326

// Create a new DWSX server which allows to connect any dApp to the wallet safely through a websocket
// Each request done by the session will wait on the appHandler and requestHandler to be accepted
// NewDWSXServer will default to forceAsk (call requestHandler) for all wallet method requests
func NewDWSXServer(
	wallet *walletapi.Wallet_Disk,
	appHandler func(*ApplicationData) bool,
	requestHandler func(*ApplicationData, *jrpc2.Request) Permission,
) *DWSX {
	return NewDWSXServerWithPort(DWSX_PORT, wallet, true, appHandler, requestHandler)
}

func NewDWSXServerWithPort(
	port int,
	wallet *walletapi.Wallet_Disk,
	forceAsk bool,
	appHandler func(*ApplicationData) bool,
	requestHandler func(*ApplicationData, *jrpc2.Request) Permission,
) *DWSX {
	mux := http.NewServeMux()
	mux.HandleFunc(
		"/",
		func(w http.ResponseWriter, r *http.Request) {
			w.Write(
				[]byte("DWSX server"),
			)
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	logger := globals.Logger.WithName("DWSX")

	// Prevent crossover of custom methods to rpcserver
	dwsxHandler := make(handler.Map)
	for k, v := range rpcserver.WalletHandler {
		dwsxHandler[k] = v
	}

	// initialize
	dwsx := &DWSX{
		applications:   make(map[*Connection]ApplicationData),
		appHandler:     appHandler,
		requestHandler: requestHandler,
		logger:         logger,
		server:         server,
		context:        rpcserver.NewWalletContext(logger, wallet),
		wallet:         wallet,
		// don't create a different API, we provide the same
		rpcHandler: dwsxHandler,
		requests:   make(chan messageRequest),
		registers:  make(chan messageRegistration),
		running:    true,
		forceAsk:   forceAsk,
		ctx:        ctx,
		cancel:     cancel,
	}

	customEvents := []rpc.EventType{
		rpc.NewBalance,
		rpc.NewTopoheight,
		rpc.NewEntry,
	}

	// Register event listeners
	dwsx.RegisterListeners(wallet, customEvents)

	// Save the server in the context
	dwsx.context.Extra["dwsx"] = dwsx

	// Define custom methods
	customMethods := map[string]handler.Func{
		"HasMethod":      handler.New(HasMethod), // HasMethod for compatibility reasons in case of custom methods declared
		"Subscribe":      handler.New(Subscribe),
		"Unsubscribe":    handler.New(Unsubscribe),
		"SignData":       handler.New(SignData),
		"CheckSignature": handler.New(CheckSignature),
		"GetDaemon":      handler.New(GetDaemon),
	}

	// Register custom methods
	dwsx.RegisterCustomMethods(customMethods)

	mux.HandleFunc("/dwsx", dwsx.handleWebSocket)
	logger.Info("Starting DWSX server", "addr", server.Addr)

	go func() {
		if err := dwsx.server.ListenAndServe(); err != nil {
			if dwsx.running {
				logger.Error(err, "Error while starting DWSX server")
				dwsx.Stop()
			}
		}
	}()

	go dwsx.handleEvents() // handlers.go

	return dwsx
}

func (x *DWSX) RegisterCustomMethods(methods map[string]handler.Func) {
	for name, handlerFunc := range methods {
		x.SetCustomMethod(name, handlerFunc)
	}
}
func (x *DWSX) RegisterListeners(w *walletapi.Wallet_Disk, events []rpc.EventType) {
	for _, eventType := range events {
		w.AddListener(
			eventType,
			func(data interface{}) {
				if x.IsEventTracked(eventType) {
					x.BroadcastEvent(
						eventType,
						data,
					)
				}
			},
		)
	}
}
func (x *DWSX) IsEventTracked(event rpc.EventType) bool {
	applications := x.GetApplications()
	for _, app := range applications {
		if app.RegisteredEvents[event] {
			return true
		}
	}

	return false
}

func (x *DWSX) BroadcastEvent(event rpc.EventType, value interface{}) {
	for conn, app := range x.applications {
		if app.RegisteredEvents[event] {
			if err := conn.Send(
				ResponseWithResult(
					nil,
					rpc.EventNotification{
						Event: event,
						Value: value,
					},
				),
			); err != nil {
				x.logger.V(2).Error(err, "Error while broadcasting event")
			}
		}
	}
}

func (x *DWSX) IsRunning() bool {
	return x.running
}

// Stop the DWSX server
// This will close all the connections
// and delete all applications
func (x *DWSX) Stop() {
	x.Lock()
	defer x.Unlock()
	x.running = false
	x.cancel()

	if err := x.server.Shutdown(context.Background()); err != nil {
		x.logger.Error(err, "Error while stopping DWSX server")
	}

	for conn, app := range x.applications {
		if app.IsRequesting() {
			app.OnClose <- true
		}

		conn.Close()
	}
	x.applications = make(map[*Connection]ApplicationData)
	x.logger.Info("DWSX server stopped")
	x = nil
}

// Register a custom method easily to be completely configurable
func (x *DWSX) SetCustomMethod(method string, handler handler.Func) {
	x.rpcHandler[method] = handler
}

// Get all connected Applications
// This will return a copy of the map
func (x *DWSX) GetApplications() []ApplicationData {
	x.Lock()
	defer x.Unlock()

	apps := make([]ApplicationData, 0, len(x.applications))
	for _, app := range x.applications {
		apps = append(apps, app)
	}

	return apps
}

// Remove an application
// It will automatically close the connection
func (x *DWSX) RemoveApplication(app *ApplicationData) {
	x.Lock()
	defer x.Unlock()

	for conn, a := range x.applications {
		if a.Id == app.Id {
			delete(x.applications, conn)
			if a.IsRequesting() {
				a.OnClose <- true
			}

			if err := conn.Close(); err != nil {
				x.logger.Error(err, "error while closing websocket session")
			}
			break
		}
	}
}

// Check if a application exist by its id
func (x *DWSX) HasApplicationId(app_id string) bool {
	x.Lock()
	defer x.Unlock()

	for _, a := range x.applications {
		if strings.EqualFold(a.Id, app_id) {
			return true
		}
	}
	return false
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}
	return true
}
