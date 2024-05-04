package dwsx

import "github.com/creachadair/jrpc2/code"

type Permission int

const (
	Ask Permission = iota
	Allow
	Deny
	AlwaysAllow
	AlwaysDeny
)

func (permission Permission) IsPositive() bool {
	return permission == Allow || permission == AlwaysAllow
}

func (permission Permission) String() string {
	var str string
	if permission == Ask {
		str = "Ask"
	} else if permission == Allow {
		str = "Allow"
	} else if permission == Deny {
		str = "Deny"
	} else if permission == AlwaysAllow {
		str = "Always Allow"
	} else if permission == AlwaysDeny {
		str = "Always Deny"
	} else {
		str = "Unknown"
	}

	return str
}

const PermissionDenied code.Code = -32043
const PermissionAlwaysDenied code.Code = -32044
