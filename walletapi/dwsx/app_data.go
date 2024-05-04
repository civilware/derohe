package dwsx

import "github.com/deroproject/derohe/rpc"

type ApplicationData struct {
	Id               string                `json:"id"`
	Name             string                `json:"name"`
	Description      string                `json:"description"`
	Url              string                `json:"url"`
	Permissions      map[string]Permission `json:"permissions"`
	Signature        []byte                `json:"signature"`
	RegisteredEvents map[rpc.EventType]bool
	// RegisteredEvents only init when accepted by user
	OnClose      chan bool `json:"-"` // used to inform when the Session disconnect
	isRequesting bool      `json:"-"`
}

func (app *ApplicationData) SetIsRequesting(value bool) {
	app.isRequesting = value
}

func (app *ApplicationData) IsRequesting() bool {
	return app.isRequesting
}
