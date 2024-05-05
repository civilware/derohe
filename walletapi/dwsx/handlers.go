package dwsx

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/code"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/walletapi"
	"github.com/gorilla/websocket"
)

func (x *DWSX) handleEvents() {
	for {
		select {
		case msg := <-x.requests:
			go x.handleRequest(msg)
		case msg := <-x.registers:
			x.handleRegistration(msg)
		case <-x.ctx.Done():
			return
		}
	}
}

func (x *DWSX) handleRequest(msg messageRequest) {
	response := x.handleMessage(msg.app, msg.request)
	if response != nil {
		if err := msg.conn.Send(response); err != nil {
			x.logger.V(2).Error(err, "Error while writing JSON", "app", msg.app.Name)
		}
	}
}

func (x *DWSX) handleRegistration(msg messageRegistration) {
	response, accepted := x.handleAddApplication(msg.request, msg.conn, msg.app)
	if accepted {
		msg.conn.Send(
			AuthorizationResponse{
				Message:  response,
				Accepted: true,
			},
		)
	} else {
		msg.conn.Send(
			AuthorizationResponse{
				Message:  "Could not connect the application: " + response,
				Accepted: false,
			},
		)
		x.handleRemoveApplicationOfSession(msg.conn, msg.app)
	}
}

// Add an application from a websocket connection,
// it verifies that application is valid and will add it to the application list if user accepts the request
func (x *DWSX) handleAddApplication(r *http.Request, conn *Connection, app *ApplicationData) (response string, accepted bool) {
	// Perform sanity checks
	if err := x.handleSanityChecks(app, r); err != nil {
		return err.Error(), false
	}

	// Check if the application ID already exists
	if x.HasApplicationId(app.Id) {
		return "Application ID already added", false
	}

	// Check user permissions and handle request
	response, accepted = x.handleUserPermission(app)
	if !accepted {
		return response, false
	}

	// Lock to handle one request at a time
	x.handlerMutex.Lock()
	defer x.handlerMutex.Unlock()

	// Register the application
	app.OnClose = make(chan bool)
	app.SetIsRequesting(true)
	if x.appHandler(app) {
		app.SetIsRequesting(false)

		// Check if the server is still running
		if !x.running {
			conn.Close()
			return "DWSX is offline", false
		}

		// Initialize registered events map
		app.RegisteredEvents = make(map[rpc.EventType]bool)

		// Add the application to the server
		x.Lock()
		defer x.Unlock()
		x.applications[conn] = *app

		return "User has authorized the application", true
	}

	app.SetIsRequesting(false)
	return "User has rejected connection request", false
}

func (x *DWSX) handleUserPermission(app *ApplicationData) (response string, accepted bool) {
	// If forceAsk all permissions will default to Ask
	if !x.forceAsk {
		// Handle permissions when not force asking
		return x.handleNonForceAskPermission(app)
	}

	// Handle permissions when force asking
	return x.handleForceAskPermission(app)
}

func (x *DWSX) handleNonForceAskPermission(app *ApplicationData) (response string, accepted bool) {
	validPermissions := make(map[string]Permission)
	normalizedMethods := make(map[string]Permission)

	for n, p := range app.Permissions {
		if strings.HasPrefix(n, "DERO.") {
			x.logger.V(1).Info("Daemon requests are AlwaysAllow", n, p)
			continue
		}

		if p == Allow || p == Deny {
			x.logger.V(1).Info("Invalid permission requested", n, p)
			continue
		}

		if _, ok := x.rpcHandler[n]; !ok {
			x.logger.V(1).Info("Invalid method requested", n, p)
			continue
		}

		normalized := strings.ToLower(strings.ReplaceAll(n, "_", ""))
		if pcheck, ok := normalizedMethods[normalized]; ok && pcheck != p {
			x.logger.V(1).Info("Conflicting permissions for", n, p)
			continue
		}

		x.logger.Info("Permission requested for", n, p)
		normalizedMethods[normalized] = p
		validPermissions[n] = p
	}

	if len(validPermissions) > 0 {
		app.Permissions = validPermissions
	} else {
		x.logger.Info("All wallet requests will Ask for your permission")
		app.Permissions = make(map[string]Permission)
	}

	return "", true
}

func (x *DWSX) handleForceAskPermission(app *ApplicationData) (response string, accepted bool) {
	x.logger.Info("All wallet requests will Ask for your permission")
	app.Permissions = make(map[string]Permission)
	return "", true
}

// Remove an application from the list for a session
// only used in internal
func (x *DWSX) handleRemoveApplicationOfSession(conn *Connection, app *ApplicationData) {
	if app != nil && app.IsRequesting() {
		x.logger.Info("App is requesting prompt, closing")
		app.OnClose <- true
	}
	conn.Close()

	x.Lock()
	application, found := x.applications[conn]
	delete(x.applications, conn)
	x.Unlock()

	if found {
		x.logger.Info("Application deleted", "id", application.Id, "name", application.Name, "description", application.Description, "url", application.Url)
	}
}

// Handle a RPC Request from a session
// We check that the method exists, that the application has the permission to use it
func (x *DWSX) handleMessage(app *ApplicationData, request *jrpc2.Request) interface{} {
	methodName := request.Method()
	handler := x.rpcHandler[methodName]

	// Check if the method exists in the RPC handler
	if handler == nil {
		return x.handleMissingMethod(request, methodName)
	}

	// Only one request at a time
	x.handlerMutex.Lock()
	defer x.handlerMutex.Unlock()

	// Check if the application is still connected
	if !x.HasApplicationId(app.Id) {
		return nil
	}

	// Check permissions
	permission := x.handleRequestPermission(app, request)
	if !permission.IsPositive() {
		return x.handlePermissionDenied(request, methodName, permission)
	}

	// Prepare wallet context
	walletContext := *x.context
	walletContext.Extra["app_data"] = app
	ctx := context.WithValue(context.Background(), "wallet_context", &walletContext)

	// Handle the request using the appropriate handler
	response, err := handler.Handle(ctx, request)
	if err != nil {
		return x.handleRequestError(request, methodName, err)
	}

	return ResponseWithResult(request, response)
}

func (x *DWSX) handleMissingMethod(request *jrpc2.Request, methodName string) interface{} {
	if strings.HasPrefix(methodName, "DERO.") {
		if x.wallet.IsDaemonOnlineCached() {
			// Request the daemon
			// Wallet acts as a proxy
			// No sensitive data can be obtained, so allow without requests
			return x.handleDaemonRequest(request)
		}
		x.logger.V(1).Info("Daemon is offline", "endpoint", x.wallet.Daemon_Endpoint)
		return ResponseWithError(request, jrpc2.Errorf(code.Cancelled, "daemon %s is offline", x.wallet.Daemon_Endpoint))
	}

	x.logger.Info("RPC Method not found", "method", methodName)
	return ResponseWithError(request, jrpc2.Errorf(code.MethodNotFound, "method %q not found", methodName))
}

func (x *DWSX) handleDaemonRequest(request *jrpc2.Request) interface{} {
	var params json.RawMessage
	err := request.UnmarshalParams(&params)
	if err != nil {
		x.logger.V(1).Error(err, "Error while unmarshaling params")
		return ResponseWithError(request, jrpc2.Errorf(code.InvalidParams, "Error while unmarshaling params: %q", err.Error()))
	}

	x.logger.V(2).Info("requesting daemon with", "method", request.Method(), "param", request.ParamString())
	result, err := walletapi.GetRPCClient().RPC.Call(context.Background(), request.Method(), params)
	if err != nil {
		x.logger.V(1).Error(err, "Error on daemon call")
		return ResponseWithError(request, jrpc2.Errorf(code.InvalidRequest, "Error on daemon call: %q", err.Error()))
	}

	// Set original ID
	result.SetID(request.ID())

	// Unmarshal result into response to sync wallet/daemon as RPCResponse type
	var response interface{}
	err = result.UnmarshalResult(&response)
	if err != nil {
		x.logger.V(1).Error(err, "Error on unmarshal daemon result")
		return ResponseWithError(request, jrpc2.Errorf(code.InternalError, "Error on unmarshal daemon call: %q", err.Error()))
	}

	json, err := result.MarshalJSON()
	if err != nil {
		x.logger.V(1).Error(err, "Error on marshal daemon response")
		return ResponseWithError(request, jrpc2.Errorf(code.InternalError, "Error on marshal daemon call: %q", err.Error()))
	}

	x.logger.V(2).Info("received response", "response", string(json))

	return ResponseWithResult(request, response)
}

func (x *DWSX) handlePermissionDenied(request *jrpc2.Request, methodName string, permission Permission) interface{} {
	errorCode := PermissionDenied
	if permission == AlwaysDeny {
		errorCode = PermissionAlwaysDenied
	}

	x.logger.Info("Permission not granted for method", "method", methodName)
	return ResponseWithError(request, jrpc2.Errorf(errorCode, "Permission not granted for method %q", methodName))
}

func (x *DWSX) handleRequestError(request *jrpc2.Request, methodName string, err error) interface{} {
	return ResponseWithError(request, jrpc2.Errorf(code.InternalError, "Error while handling request method %q: %v", methodName, err))
}

// Request the permission for a method and save its result if it must be persisted
func (x *DWSX) handleRequestPermission(app *ApplicationData, request *jrpc2.Request) Permission {
	permission, found := app.Permissions[request.Method()]
	if !found || permission == Ask {
		permission = x.requestHandler(app, request)

		if permission == AlwaysDeny || permission == AlwaysAllow {
			app.Permissions[request.Method()] = permission
		}

		if permission.IsPositive() {
			x.logger.Info(
				"Permission granted",
				"method",
				request.Method(),
				"permission",
				permission,
			)
		} else {
			x.logger.Info(
				"Permission rejected",
				"method",
				request.Method(),
				"permission",
				permission,
			)
		}
	} else {
		x.logger.V(1).Info(
			"Permission already granted for method",
			"method",
			request.Method(),
			"permission",
			permission,
		)
	}

	return permission
}

// block until the session is closed and read all its messages
func (x *DWSX) handleReadMessageFromSession(conn *Connection, app *ApplicationData) {
	defer x.handleRemoveApplicationOfSession(conn, app)

	for {
		// block and read the message bytes from session
		_, buff, err := conn.Read()
		if err != nil {
			x.logger.V(2).Error(err, "Error while reading message from session")
			return
		}

		// app tried to send us a request while he was not authorized yet
		if !x.HasApplicationId(app.Id) {
			x.logger.Info("App is not authorized and requests us, closing")
			return
		}

		// unmarshal the request
		requests, err := jrpc2.ParseRequests(buff)
		if err != nil {
			x.logger.Error(err, "Error while parsing request")
			if err := conn.Send(
				ResponseWithError(
					nil,
					jrpc2.Errorf(
						code.ParseError,
						"Error while parsing request",
					),
				),
			); err != nil {
				return
			}
			continue
		}

		request := requests[0]
		// We only support one request at a time for permission request
		if len(requests) != 1 {
			x.logger.V(2).Error(nil, "Invalid number of requests")
			if err := conn.Send(
				ResponseWithError(
					nil,
					jrpc2.Errorf(
						code.ParseError,
						"Batch requests are not supported",
					),
				),
			); err != nil {
				return
			}
			continue
		}

		x.requests <- messageRequest{
			app:     app,
			request: request,
			conn:    conn,
		}
	}
}

// Handle a WebSocket connection
func (x *DWSX) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	globals.Logger.V(2).Info("New WebSocket connection", "addr", r.RemoteAddr)
	// Accept from any origin
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		x.logger.V(1).Error(err, "WebSocket upgrade error")
		return
	}
	defer conn.Close()

	// first message of the session should be its ApplicationData
	var app_data ApplicationData
	if err := conn.ReadJSON(&app_data); err != nil {
		x.logger.V(2).Error(err, "Error while reading app_data")
		conn.WriteJSON(
			AuthorizationResponse{
				Message:  "Invalid app data format",
				Accepted: false,
			},
		)

		return
	}

	if x.HasApplicationId(app_data.Id) {
		x.logger.Info("App ID is already used", "ID", app_data.Name)
		conn.WriteJSON(
			AuthorizationResponse{
				Message:  "App ID is already used",
				Accepted: false,
			},
		)

		return
	}

	connection := new(Connection)
	connection.conn = conn
	x.registers <- messageRegistration{
		conn:    connection,
		request: r,
		app:     &app_data,
	}

	x.handleReadMessageFromSession(connection, &app_data)
}

func (x *DWSX) handleSanityChecks(app *ApplicationData, r *http.Request) error {
	checks := []func() error{
		func() error { return checkID(app.Id) },
		func() error { return checkName(app.Name) },
		func() error { return checkDescription(app.Description) },
		func() error { return checkURL(x, app.Url, r) },
		func() error { return checkSignature(x, app) },
		func() error { return checkPermissions(app) },
	}

	for _, check := range checks {
		if err := check(); err != nil {
			return err
		}
	}

	return nil
}

func checkID(id string) error {
	id = strings.TrimSpace(id)
	if len(id) != 64 {
		return fmt.Errorf("invalid ID size: %s", id)
	}

	if _, err := hex.DecodeString(id); err != nil {
		return fmt.Errorf("invalid hexadecimal ID: %s", id)
	}

	return nil
}

func checkName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > 255 || !isASCII(name) {
		return fmt.Errorf("invalid name: %s", name)
	}

	return nil
}

func checkDescription(description string) error {
	description = strings.TrimSpace(description)
	if len(description) == 0 || len(description) > 255 || !isASCII(description) {
		return fmt.Errorf("invalid description: %s", description)
	}

	return nil
}

func checkURL(x *DWSX, url string, r *http.Request) error {
	url = strings.TrimSpace(url)
	if len(url) == 0 {
		url = r.Header.Get("Origin")
		if len(url) > 0 {
			x.logger.V(1).Info("No URL passed, checking origin header")
		}
	}

	origin := r.Header.Get("Origin")
	if len(origin) > 0 && url != origin {
		return fmt.Errorf("invalid URL compared to origin: origin=%s, url=%s", origin, url)
	}

	if len(url) > 255 || !(strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) {
		return fmt.Errorf("invalid URL: %s", url)
	}

	return nil
}

func checkSignature(x *DWSX, app *ApplicationData) error {
	if len(app.Signature) == 0 {
		return nil // No signature provided, which is acceptable
	}

	// Check signature size
	if len(app.Signature) > 512 {
		return fmt.Errorf("invalid signature size: %d", len(app.Signature))
	}

	// Validate signature
	signer, message, err := x.wallet.CheckSignature(app.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature: %s", string(app.Signature))
	}

	// Check signer network
	if !signer.IsDERONetwork() {
		return fmt.Errorf("signer does not belong to DERO network: signer=%s", signer.String())
	}

	// Verify signature matches app ID
	msgcheck := strings.TrimSpace(string(message))
	if msgcheck != app.Id {
		return fmt.Errorf("signature does not match ID: signature=%s, id=%s", app.Signature, app.Id)
	}

	x.logger.V(1).Info("signature matches ID", app.Id, msgcheck)
	return nil
}

func checkPermissions(app *ApplicationData) error {
	if len(app.Permissions) == 0 {
		return nil // No permissions, which is acceptable
	}

	// Check permissions size
	if len(app.Permissions) > 255 {
		return fmt.Errorf("invalid permissions: %d", len(app.Permissions))
	}

	// Check if permissions provided without signature
	if len(app.Signature) == 0 {
		return errors.New("application is requesting permissions without signature")
	}

	return nil
}
