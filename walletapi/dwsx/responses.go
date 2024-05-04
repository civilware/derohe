package dwsx

import "github.com/creachadair/jrpc2"

type RPCResponse struct {
	JsonRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

func ResponseWithError(request *jrpc2.Request, err *jrpc2.Error) RPCResponse {
	var id string
	if request != nil {
		id = request.ID()
	}

	return RPCResponse{
		JsonRPC: "2.0",
		ID:      id,
		Error:   err,
	}
}

func ResponseWithResult(request *jrpc2.Request, result interface{}) RPCResponse {
	var id string
	if request != nil {
		id = request.ID()
	}

	return RPCResponse{
		JsonRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

type AuthorizationResponse struct {
	Message  string `json:"message"`
	Accepted bool   `json:"accepted"`
}
