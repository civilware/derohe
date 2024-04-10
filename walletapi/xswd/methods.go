package xswd

import (
	"context"
	"fmt"
	"strings"

	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/walletapi"
	"github.com/deroproject/derohe/walletapi/rpcserver"
)

type HasMethod_Params struct {
	Name string `json:"name"`
}

type Subscribe_Params struct {
	Event rpc.EventType `json:"event"`
}

type Signature_Result struct {
	Signer  string `json:"signer"`
	Message string `json:"message"`
}

func HasMethod(ctx context.Context, p HasMethod_Params) bool {
	w := rpcserver.FromContext(ctx)
	xswd := w.Extra["xswd"].(*XSWD)
	_, ok := xswd.rpcHandler[p.Name]
	return ok
}

func Subscribe(ctx context.Context, p Subscribe_Params) bool {
	w := rpcserver.FromContext(ctx)
	app := w.Extra["app_data"].(*ApplicationData)

	_, ok := app.RegisteredEvents[p.Event]
	if ok {
		return false
	}

	app.RegisteredEvents[p.Event] = true

	return true
}

func Unsubscribe(ctx context.Context, p Subscribe_Params) bool {
	w := rpcserver.FromContext(ctx)
	app := w.Extra["app_data"].(*ApplicationData)

	_, ok := app.RegisteredEvents[p.Event]
	if !ok {
		return false
	}

	delete(app.RegisteredEvents, p.Event)

	return true
}

// SignData returned as DERO signed message
func SignData(ctx context.Context, p []byte) (signed []byte, err error) {
	w := rpcserver.FromContext(ctx)
	xswd := w.Extra["xswd"].(*XSWD)
	if xswd.wallet == nil {
		err = fmt.Errorf("XSWD could not sign data")
		return
	}

	signed = xswd.wallet.SignData(p)

	return
}

// CheckSignature of DERO signed message
func CheckSignature(ctx context.Context, p []byte) (result Signature_Result, err error) {
	w := rpcserver.FromContext(ctx)
	xswd := w.Extra["xswd"].(*XSWD)
	if xswd.wallet == nil {
		err = fmt.Errorf("XSWD could not check signature")
		return
	}

	var address *rpc.Address
	var messageBytes []byte
	address, messageBytes, err = xswd.wallet.CheckSignature(p)
	if err != nil {
		return
	}

	result.Signer = address.String()
	result.Message = strings.TrimSpace(string(messageBytes))

	return
}

// GetDaemon endpoint from connected wallet
func GetDaemon(ctx context.Context) (result string, err error) {
	if walletapi.Daemon_Endpoint_Active != "" {
		result = walletapi.Daemon_Endpoint_Active
	} else {
		err = fmt.Errorf("XSWD could not get daemon endpoint from wallet")
	}

	return
}
