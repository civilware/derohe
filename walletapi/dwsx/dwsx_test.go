package dwsx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/code"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/walletapi"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/ybbus/jsonrpc"
)

//

const validTo = 7

// Test AppData
// Apps 0 to 6 are valid apps and above validTo are invalid,
// 1 and 2 have valid permission requests that will be set,
// 3 has valid and invalid permissions requested,
// 5 and 6 have conflicting permissions, the other valid apps should default to Ask
var testAppData = []ApplicationData{
	// // App 0
	// Valid test app data without signature or permission requests
	{
		Id:          "76a16407d9371ebcb57b3009ba7a0e705314e23b7d220df635788d2e88052dab",
		Name:        "Test App0",
		Description: "Zero application",
		Url:         "http://testapp0.com",
	},
	// // App 1
	// Valid test app data with signature and permission requests
	{
		Id:          "031109fd406e1f76ca61a14ce1cd73a31bf832b99d64b8906f7d612ec8b4c8c7",
		Name:        "Test App1",
		Description: "One application",
		Url:         "http://testapp1.com",
		Permissions: map[string]Permission{ // Only Ask, AlwaysDeny and AlwaysAllow will be stored
			"GetHeight":    AlwaysDeny,
			"GetAddress":   AlwaysAllow,
			"GetTransfers": Allow,
			"transfer":     Deny,
			"GetBalance":   Ask,
		},
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: 2f5240134cf4ffe0ee8ad08331ef260c1082091e199b4f4f05d8d4ac2c7a4537
S: 21363063edd2f9368f2cff38be7684e73031f138956e34c399d98eac47fba255

MDMxMTA5ZmQ0MDZlMWY3NmNhNjFhMTRjZTFjZDczYTMxYmY4MzJiOTlkNjRiODkw
NmY3ZDYxMmVjOGI0YzhjNw==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 2
	// Valid test app data with signature and permission requests
	{
		Id:          "e162616036e5d6fb2d491ed8edb415fbc49a2801d15da08c99e4a5e087e360d7",
		Name:        "Test App2",
		Description: "Two application",
		Url:         "http://testapp2.com",
		Permissions: map[string]Permission{ // Only Ask, AlwaysDeny and AlwaysAllow will be stored
			"Transfer":   AlwaysDeny,
			"GetAddress": Allow,
			"GetHeight":  Deny,
			"getbalance": Ask, // Double method store equal value
			"GetBalance": Ask,
		},
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: 21806c86ddac7aa08e22d2776c7f9b4f459bd506ed0fab42f5b4059aef800c58
S: 159d4bf4dfa5277a56ad5da5f04864dc7112190907f963eaf753945d894ee956

ZTE2MjYxNjAzNmU1ZDZmYjJkNDkxZWQ4ZWRiNDE1ZmJjNDlhMjgwMWQxNWRhMDhj
OTllNGE1ZTA4N2UzNjBkNw==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 3
	// Valid test app data with signature and invalid permission requests
	{
		Id:          "68d1b2a6faecf7d402f40584a59dccaf80e9dea6b82aba817239f3e60cfbc3e3",
		Name:        "Test App3",
		Description: "Three application",
		Url:         "http://testapp3.com",
		Permissions: map[string]Permission{ // Custom methods should not be stored
			"Get":            AlwaysDeny,
			"Send":           AlwaysAllow,
			"Engram":         Allow,
			"Netrunner":      Deny,
			"Artificer":      Ask,
			"GetDaemon":      AlwaysAllow, // Only store methods from rpcserver/dwsx
			"SignData":       AlwaysAllow,
			"CheckSignature": AlwaysAllow,
		},
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: b340eb3c25a903ed972053e9f88817b4bc789221620e8f60163d55ddc52b07b
S: 11df7c5d06d5608497621a4526110551f17da88f05b94df79c0662dbd0ac753a

NjhkMWIyYTZmYWVjZjdkNDAyZjQwNTg0YTU5ZGNjYWY4MGU5ZGVhNmI4MmFiYTgx
NzIzOWYzZTYwY2ZiYzNlMw==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 4
	// Valid test app data with no permissions
	{
		Id:          "4d3b4bd59f62809cec118f90c0b3548b95e26c3b985e8f71849be8d2986b34a9",
		Name:        "Test App4",
		Description: "Four application",
		Url:         "http://testapp4.com",
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: 2f9f7533b831ea8724c58a7400e72103f3550683cd8861a7ea3ba4b2bae4892a
S: 13de6902031b6afae79ed2cb7c71861841d53b02705adbfe40c59881ad5263d0

NGQzYjRiZDU5ZjYyODA5Y2VjMTE4ZjkwYzBiMzU0OGI5NWUyNmMzYjk4NWU4Zjcx
ODQ5YmU4ZDI5ODZiMzRhOQ==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 5
	// Valid test app data with signature and conflicting permission requests
	{
		Id:          "f56e7535df9d97e7ce629fd6e6b2b1fccf9d2335eaf77a8a475f0d008c6790a3",
		Name:        "Test App5",
		Description: "Five application",
		Url:         "http://testapp5.com",
		Permissions: map[string]Permission{ // Conflicting, should store equal value for valid permissions
			"GetHeight":     Ask,
			"getheight":     AlwaysAllow,
			"get_transfers": Ask,
			"GetTransfers":  Allow,
			"transfer":      Deny,
			"Transfer":      AlwaysDeny,
			"DERO.Ping":     AlwaysDeny, // Daemon permissions requested should not be stored
			"DERO.GetInfo":  Ask,
		},
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: aeffb5c7348a36fc00a115cb61cd551965649e93805c434213e8073b5ae0162
S: 2c55d1644e5ec948fe4af8395aa3e86f1ce6c06e9db08cbeede105528bf0876e

ZjU2ZTc1MzVkZjlkOTdlN2NlNjI5ZmQ2ZTZiMmIxZmNjZjlkMjMzNWVhZjc3YThh
NDc1ZjBkMDA4YzY3OTBhMw==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 6
	// Valid test app data with signature and conflicting permission requests
	{
		Id:          "00788e91f9418e26bfc6e44a6f57b48722e7860785f37b0b3ae2786274358c60",
		Name:        "Test App6",
		Description: "Six application",
		Url:         "http://testapp6.com",
		Permissions: map[string]Permission{ // Conflicting, should store equal value for valid permissions
			"Transfer":       AlwaysDeny,
			"transfer":       Ask,
			"transfer_split": AlwaysAllow,
			"GetAddress":     Allow,
			"getaddress":     Deny,
			"getbalance":     Ask,
			"GetBalance":     AlwaysAllow,
		},
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: 8f0692c7c5500871a5b2e084720610069d8a2cc9a04df9a62e768ce0fa1a47e
S: 11572410681e3a92482115276c4546896425f6479010ca3a3fab0c13eef5018

MDA3ODhlOTFmOTQxOGUyNmJmYzZlNDRhNmY1N2I0ODcyMmU3ODYwNzg1ZjM3YjBi
M2FlMjc4NjI3NDM1OGM2MA==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 7
	// Invalid test app data, signature is invalid
	{
		Id:          "afa13ff5281d84548cfe0dcccc4c245467b2172c18b04cfce985dc53feb65a1f",
		Name:        "Test App7",
		Description: "Seven application",
		Url:         "http://testapp7.com",
		Permissions: map[string]Permission{ // Nothing should be stored
			"GetHeight":    AlwaysDeny,
			"GetAddress":   AlwaysAllow,
			"GetTransfers": Allow,
			"transfer":     Deny,
			"GetBalance":   Ask,
		},
		// Valid signature if aligned
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
		Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
		C: 1436a038538330c9f2ee5612727f14723f0554720c96fe859fa92553d02aa999
		S: 141e127d4c43ce57da832c8cef171ba4ffb74eee62f7c1fc3f1a45f717d7533
		
		YWZhMTNmZjUyODFkODQ1NDhjZmUwZGNjY2M0YzI0NTQ2N2IyMTcyYzE4YjA0Y2Zj
		ZTk4NWRjNTNmZWI2NWExZg==
		-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 8
	// Invalid test app data signature ID mismatch
	{
		Id:          "4d3b4bd59f62809cec118f90c0b3548b95e26c3b985e8f71849be8d2986b34a9",
		Name:        "Test App8",
		Description: "Eight application",
		Url:         "http://testapp8.com",
		Permissions: map[string]Permission{ // Nothing should be stored
			"GetHeight":    AlwaysDeny,
			"GetAddress":   AlwaysAllow,
			"GetTransfers": Allow,
			"transfer":     Deny,
			"GetBalance":   Ask,
		},
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: 1436a038538330c9f2ee5612727f14723f0554720c96fe859fa92553d02aa999
S: 141e127d4c43ce57da832c8cef171ba4ffb74eee62f7c1fc3f1a45f717d7533

YWZhMTNmZjUyODFkODQ1NDhjZmUwZGNjY2M0YzI0NTQ2N2IyMTcyYzE4YjA0Y2Zj
ZTk4NWRjNTNmZWI2NWExZg==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 9
	// Invalid test app data, invalid ID
	{
		Id:          "",
		Name:        "Test App9",
		Description: "Nine application",
		Url:         "http://testapp9.com",
		Permissions: map[string]Permission{ // Nothing should be stored
			"GetHeight":    AlwaysDeny,
			"GetAddress":   AlwaysAllow,
			"GetTransfers": Allow,
			"transfer":     Deny,
			"GetBalance":   Ask,
		},
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: 1436a038538330c9f2ee5612727f14723f0554720c96fe859fa92553d02aa999
S: 141e127d4c43ce57da832c8cef171ba4ffb74eee62f7c1fc3f1a45f717d7533

YWZhMTNmZjUyODFkODQ1NDhjZmUwZGNjY2M0YzI0NTQ2N2IyMTcyYzE4YjA0Y2Zj
ZTk4NWRjNTNmZWI2NWExZg==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 10
	// Invalid test app data, invalid URL
	{
		Id:          "afa13ff5281d84548cfe0dcccc4c245467b2172c18b04cfce985dc53feb65a1f",
		Name:        "Test App10",
		Description: "Ten application",
		Url:         "",
		Permissions: map[string]Permission{ // Nothing should be stored
			"GetHeight":    AlwaysDeny,
			"GetAddress":   AlwaysAllow,
			"GetTransfers": Allow,
			"transfer":     Deny,
			"GetBalance":   Ask,
		},
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: 1436a038538330c9f2ee5612727f14723f0554720c96fe859fa92553d02aa999
S: 141e127d4c43ce57da832c8cef171ba4ffb74eee62f7c1fc3f1a45f717d7533

YWZhMTNmZjUyODFkODQ1NDhjZmUwZGNjY2M0YzI0NTQ2N2IyMTcyYzE4YjA0Y2Zj
ZTk4NWRjNTNmZWI2NWExZg==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 11
	// Invalid test app data, without signature requesting permissions
	{
		Id:          "1ed8e45c96ee5134968d5ada8ebeca25323db543b1bacc6a8e62f0803f205c21",
		Name:        "Test App11",
		Description: "Eleven application",
		Url:         "http://testapp11.com",
		Permissions: map[string]Permission{ // Nothing should be stored
			"GetHeight":    AlwaysDeny,
			"GetAddress":   AlwaysAllow,
			"GetTransfers": Allow,
			"transfer":     Deny,
			"GetBalance":   Ask,
		},
	},
	// // App 12
	// Invalid test app data, signature is to long
	{
		Id:          "afa13ff5281d84548cfe0dcccc4c245467b2172c18b04cfce985dc53feb65a1f",
		Name:        "Test App12",
		Description: "Twelve application",
		Url:         "http://testapp12.com",
		Permissions: map[string]Permission{
			"GetHeight":    AlwaysDeny,
			"GetAddress":   AlwaysAllow,
			"GetTransfers": Allow,
			"transfer":     Deny,
			"GetBalance":   Ask,
		},
		// Signature to long
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: 1436a038538330c9f2ee5612727f14723f0554720c96fe859fa92553d02aa999
S: 141e127d4c43ce57da832c8cef171ba4ffb74eee62f7c1fc3f1a45f717d7533

YWZhMTNmZjUyODFkODQ1NDhjZmUwZGNjY2M0YzI0NTQ2N2IyMTcyYzE4YjA0Y2Zj
ZTk4NWRjNTNmZWI2NWExZg==
-----END DERO SIGNED MESSAGE----------BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: 1436a038538330c9f2ee5612727f14723f0554720c96fe859fa92553d02aa999
S: 141e127d4c43ce57da832c8cef171ba4ffb74eee62f7c1fc3f1a45f717d7533

YWZhMTNmZjUyODFkODQ1NDhjZmUwZGNjY2M0YzI0NTQ2N2IyMTcyYzE4YjA0Y2Zj
ZTk4NWRjNTNmZWI2NWExZg==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 13
	// Invalid test app data, invalid hexadecimal ID
	{
		Id:          "123456789012345678901234567890123456789012345678901234567890123x",
		Name:        "Test App13",
		Description: "Thirteen application",
		Url:         "http://testapp13.com",
	},
	// // App 14
	// Invalid test app data, invalid Name
	{
		Id:          "afa13ff5281d84548cfe0dcccc4c245467b2172c18b04cfce985dc53feb65a1f",
		Name:        "",
		Description: "Fourteen application",
		Url:         "http://testapp14.com",
	},
	// // App 15
	// Invalid test app data, description !isASCII
	{
		Id:          "afa13ff5281d84548cfe0dcccc4c245467b2172c18b04cfce985dc53feb65a1f",
		Name:        "Test App15",
		Description: "💻💻",
		Url:         "http://testapp15.com",
	},
	// // App 16
	// Invalid test app data, URL to long
	{
		Id:          "afa13ff5281d84548cfe0dcccc4c245467b2172c18b04cfce985dc53feb65a1f",
		Name:        "Test App16",
		Description: "Sixteen application",
		Url:         "https://www.testapp16.com/api/v1/resource?param1=value1&param2=value2&param3=value3&param4=value4&param5=value5&param6=value6&param7=value7&param8=value8&param9=value9&param10=value10&param11=value11&param12=value12&param13=value13&param14=value14&param15=value15",
	},
	// // App 17
	// Invalid test app data, will be loaded with excess permissions before connection request
	{
		Id:          "68d1b2a6faecf7d402f40584a59dccaf80e9dea6b82aba817239f3e60cfbc3e3",
		Name:        "Test App3",
		Description: "Three application",
		Url:         "http://testapp3.com",
		Permissions: make(map[string]Permission),
		Signature: []byte(`-----BEGIN DERO SIGNED MESSAGE-----
Address: deto1qyvyeyzrcm2fzf6kyq7egkes2ufgny5xn77y6typhfx9s7w3mvyd5qqynr5hx
C: b340eb3c25a903ed972053e9f88817b4bc789221620e8f60163d55ddc52b07b
S: 11df7c5d06d5608497621a4526110551f17da88f05b94df79c0662dbd0ac753a

NjhkMWIyYTZmYWVjZjdkNDAyZjQwNTg0YTU5ZGNjYWY4MGU5ZGVhNmI4MmFiYTgx
NzIzOWYzZTYwY2ZiYzNlMw==
-----END DERO SIGNED MESSAGE-----`),
	},
	// // App 18
	// Invalid data
	{},
}

// Test wallet data from walletapi for DWSX wallet test
var testWalletData = []struct {
	lang       string
	seed       string
	secret_key string
	public_key string
	Address    string
}{
	{
		lang:       "English",
		seed:       "sequence atlas unveil summon pebbles tuesday beer rudely snake rockets different fuselage woven tagged bested dented vegan hover rapid fawns obvious muppet randomly seasons randomly",
		secret_key: "b0ef6bd527b9b23b9ceef70dc8b4cd1ee83ca14541964e764ad23f5151204f0f",
		public_key: "09d704feec7161952a952f306cd96023810c6788478a1c9fc50e7281ab7893ac01",
		Address:    "deto1qyyawp87a3ckr9f2j5hnqmxevq3czrr83prc58ylc5889qdt0zf6cqg26e27g",
	},
}

func TestMain(m *testing.M) {
	// Add excess permissions to app 17
	for i := 0; i < 256; i++ {
		testAppData[17].Permissions[fmt.Sprintf("%d", i)] = AlwaysAllow
	}
	m.Run()
}

// TestDWSXServer tests request using each permission type inside requestHandler and invalid method
func TestDWSXServer(t *testing.T) {
	// Simulate user denying the application connection request for Disconnected tests
	var userApproval, userPort bool = false, false

	// Using Allow permission request, in Disconnected tests it should fail before it is requested
	var appRequest Permission = Allow

	// Create DWSX server
	var err error
	var server = &DWSX{}
	assert.False( // is the server running?
		t,
		server.IsRunning(),
		"DWSX server should not be running and is",
	)
	// Using NewDWSXServer, which defaults all permissions to Ask
	wallet, server, err := newDWSXServer(
		t,
		userPort,     // false
		userApproval, // false
		appRequest,   // Asking for User to Allow
	)

	// check for err in operation
	assert.NoErrorf(
		t,
		err,
		"newDWSXServer should not error: %s",
		err,
	)

	// check wallet for err
	assert.Exactly(
		t,
		wallet.Error,
		nil,
	)

	// Stop server and ensure it is not running
	defer func() {
		server.Stop()
		assert.False(t, server.IsRunning(), "DWSX server should not be running and is")
	}()

	// Tests requests made when application is not connected to the server
	t.Run(
		"Disconnected",
		func(t *testing.T) {
			assert.Len(t, server.GetApplications(), 0, "There should be no applications")
			// Loop through testAppData. 0-6 are valid apps, above is not
			for i, app := range testAppData {
				// Create a websocket client to connect to the server
				conn, err := testCreateClient(nil)
				assert.NoErrorf(t, err, "Application %d failed to dial server: %s", i, err)

				// Send ApplicationData to server
				err = conn.WriteJSON(app)
				assert.NoErrorf(t, err, "Application %d failed to write data to server: %s", i, err)

				authResponse := testHandleAuthResponse(t, conn)
				t.Logf("Authorization %d response: %v", i, authResponse.Message)
				assert.False(t, authResponse.Accepted, "Application %d should not be accepted and is", i)

				// Was application added to the server
				assert.Len(t, server.GetApplications(), 0, "Application %d should not be present and is", i)

				// // Request 1
				t.Run(
					"Request1",
					func(t *testing.T) {
						// GetAddress should fail as our connection request had not been accepted
						request1 := jsonrpc.RPCRequest{
							JSONRPC: "2.0",
							ID:      1,
							Method:  "GetAddress",
						}
						response1, serverErr, err := testDWSXCall(t, conn, request1)
						assert.Error(t, err, "Request 1 %q should give error when not connected with application %d", request1.Method, i)
						assert.NotNil(t, response1, "Response 1 should not be nil when not connected")
						assert.Nil(t, serverErr, "Response 1 should not have error: %v", serverErr)
					},
				)

				// // Request 2
				t.Run(
					"Request2",
					func(t *testing.T) {
						// DERO.Ping should fail as our connection request had not been accepted
						request2 := jsonrpc.RPCRequest{
							JSONRPC: "2.0",
							ID:      1,
							Method:  "DERO.Ping",
						}
						err = conn.WriteJSON(request2)
						assert.Error(t, err, "Request 2 %q should give error when not connected with application %d", request2.Method, i)
					},
				)

				// // Request 3
				t.Run(
					"Request3",
					func(t *testing.T) {
						// Invalid json data
						request3 := jsonrpc.RPCRequest{
							JSONRPC: "request",
							ID:      9,
							Method:  "GetAddress",
						}
						err = conn.WriteJSON(request3)
						assert.Error(t, err, "Request 3 %q should give error when not connected with application %d", request3.Method, i)
					},
				)

				// // Request 4
				t.Run(
					"Request4",
					func(t *testing.T) {
						// QueryKey should fail as our connection request had not been accepted
						request4 := jsonrpc.RPCRequest{
							JSONRPC: "2.0",
							ID:      1,
							Method:  "QueryKey",
						}
						err = conn.WriteJSON(request4)
						assert.Error(t, err, "Request 4 %q should give error when not connected with application %d", request4.Method, i)
					},
				)

				// // Request 5 request
				t.Run(
					"Request5",
					func(t *testing.T) {
						somedata := []byte(app.Id)
						// SignData should fail as our connection request had not been accepted
						request5 := jsonrpc.RPCRequest{
							JSONRPC: "2.0",
							ID:      1,
							Method:  "SignData",
							Params:  somedata,
						}
						err = conn.WriteJSON(request5)
						assert.Error(t, err, "Request 5 %q should give error when not connected with application %d", request5.Method, i)
					},
				)

				// Close the app connection
				conn.Close()
				time.Sleep(10 * time.Millisecond)

				// Ensure there is no apps as connection was closed
				assert.Len(t, server.GetApplications(), 0, "There should be no applications")
			}
		},
	)

	// Tests requests while connected using each permission type inside requestHandler and invalid data requests
	t.Run("Connected", func(t *testing.T) {
		assert.Len(t, server.GetApplications(), 0, "There should be no applications")
		// Simulate user accepting the application connection request to server
		server.appHandler = func(ad *ApplicationData) bool { return true }

		// Simulate Allow permission request to server
		server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return Allow }

		// Loop through testAppData. 0-6 are valid apps, above is not
		for i, app := range testAppData {
			// Create a websocket client to connect to the server
			conn, err := testCreateClient(nil)
			assert.NoErrorf(t, err, "Application %d failed to dial server: %s", i, err)

			// Send ApplicationData to server
			err = conn.WriteJSON(app)
			assert.NoErrorf(t, err, "Application %d failed to write data to server: %s", i, err)

			authResponse := testHandleAuthResponse(t, conn)
			t.Logf("Authorization %d response: %v", i, authResponse.Message)
			if i < validTo {
				assert.True(t, authResponse.Accepted, "Application %d should be accepted and is not", i)
				// Was application added to the server
				assert.Len(t, server.GetApplications(), 1, "Application %d should be present and is not", i)
				assert.True(t, server.HasApplicationId(app.Id), "Application ID %d should be present and is not", i)
				// Check that no permissions have been added to the server
				for ii, appp := range server.applications {
					assert.Len(t, appp.Permissions, 0, "Application %d should not have permissions: %v", ii, appp.Permissions)
				}
			} else {
				assert.False(t, authResponse.Accepted, "Application %d should not be accepted and is", i)
				assert.Len(t, server.GetApplications(), 0, "Application %d should not be present and is", i)
				continue
			}

			// // Request 0
			t.Run("Request0", func(t *testing.T) {
				// Echo should return successfully as requestHandler is Allow
				params0 := []string{"DERO", "Test", "DWSX"}
				request0 := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "Echo",
					Params:  params0,
				}

				response0, serverErr, err := testDWSXCall(t, conn, request0)
				assert.NoErrorf(t, err, "Request 0 %q on application %d should not error: %s", request0.Method, i, err)
				assert.NotNil(t, response0, "Response 0 on application %d should not be nil", i)
				assert.Nil(t, serverErr, "Response 0 on application %d should not have error: %v", i, serverErr)
				assert.Equal(t, fmt.Sprintf("WALLET %s", strings.Join(params0, " ")), response0.Result, "Response 0 on application %d does not match expected: %v", i, response0.Result)
			})

			// // Request 1
			t.Run("Request1", func(t *testing.T) {
				// GetAddress should return successfully as requestHandler is Allow
				request1 := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "GetAddress",
				}

				response1, serverErr, err := testDWSXCall(t, conn, request1)
				assert.NoErrorf(t, err, "Request 1 %q on application %d should not error: %s", request1.Method, i, err)
				assert.NotNil(t, response1, "Response 1 on application %d should not be nil", i)
				assert.Nil(t, serverErr, "Response 1 on application %d should not have error: %v", i, serverErr)
				// Ensure address matches
				assert.IsType(t, map[string]interface{}{}, response1.Result, "Response 1 should be map[string]interface{}: %T", i, response1.Result)
				assert.Equal(t, testWalletData[0].Address, response1.Result.(map[string]interface{})["address"].(string))
			})

			// // Request 2
			t.Run("Request2", func(t *testing.T) {
				// Deny GetHeight request should not be successful
				server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return Deny }
				request2a := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "GetHeight",
				}

				response2a, serverErr, err := testDWSXCall(t, conn, request2a)
				assert.NoErrorf(t, err, "Request 2a %q on application %d should not error: %s", request2a.Method, i, err)
				assert.NotNil(t, response2a, "Response 2a on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 2a on application %d should have error as permission was Deny: %v", i, serverErr)
				assert.Equal(t, PermissionDenied, serverErr.Code, "Response 2a on application %d should be %v: %v", i, PermissionDenied, serverErr.Code)

				// Deny QueryKey request should not be successful
				request2b := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "QueryKey",
				}
				response2b, serverErr, err := testDWSXCall(t, conn, request2b)
				assert.NoErrorf(t, err, "Request 2b %q on application %d should not error: %s", request2b.Method, i, err)
				assert.NotNil(t, response2b, "Response 2b on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 2b on application %d should have error as permission was Deny: %v", i, serverErr)
				assert.Equal(t, PermissionDenied, serverErr.Code, "Response 2b on application %d should be %v: %v", i, PermissionDenied, serverErr.Code)
			})

			// // Request 3
			t.Run("Request3", func(t *testing.T) {
				// AlwaysAllow GetTransfers request should be successful
				server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return AlwaysAllow }
				request3 := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "GetTransfers",
					Params: rpc.Get_Transfers_Params{
						Coinbase:        false,
						In:              false,
						Out:             false,
						Min_Height:      0,
						Max_Height:      0,
						Sender:          "",
						Receiver:        "",
						DestinationPort: 0,
						SourcePort:      0,
					},
				}

				// Call once to set AlwaysAllow
				response3a, serverErr, err := testDWSXCall(t, conn, request3)
				assert.NoErrorf(t, err, "Request 3a %q on application %d should not error: %s", request3.Method, i, err)
				assert.NotNil(t, response3a, "Response 3a on application %d should not be nil", i)
				assert.Nil(t, serverErr, "Response 3a on application %d should not have error: %v", i, serverErr)

				// Set requestHandler to Deny but should be successful if called again as was AlwaysAllowed
				server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return Deny }
				// Call again
				response3b, serverErr, err := testDWSXCall(t, conn, request3)
				assert.NoErrorf(t, err, "Request 3b %q on application %d should not error: %s", request3.Method, i, err)
				assert.NotNil(t, response3b, "Response 3b on application %d should not be nil", i)
				assert.Nil(t, serverErr, "Response 3b on application %d should not have error: %v", i, serverErr)
			})

			// // Request 4
			t.Run("Request4", func(t *testing.T) {
				// Echo AlwaysDeny should not be successful
				server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return AlwaysDeny }
				request4 := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "Echo",
				}

				// Call and set AlwaysDeny
				response4a, serverErr, err := testDWSXCall(t, conn, request4)
				assert.NoErrorf(t, err, "Request 4a %q on application %d should not error: %s", request4.Method, i, err)
				assert.NotNil(t, response4a, "Response 4a on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 4a on application %d should have error as permission was AlwaysDeny: %v", i, serverErr)
				assert.Equal(t, PermissionAlwaysDenied, serverErr.Code, "Response 4a on application %d should be %v: %v", i, PermissionAlwaysDenied, serverErr.Code)

				// Set requestHandler to Allow but should not be successful if called again as was AlwaysDenied
				server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return Allow }
				// Call again
				response4b, serverErr, err := testDWSXCall(t, conn, request4)
				assert.NoErrorf(t, err, "Request 4b %q on application %d should not error: %s", request4.Method, i, err)
				assert.NotNil(t, response4b, "Response 4b on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 4a on application %d should have error as permission was AlwaysDeny: %v", i, serverErr)
				assert.Equal(t, PermissionAlwaysDenied, serverErr.Code, "Response 4b on application %d should be %v: %v", i, PermissionAlwaysDenied, serverErr.Code)
			})

			// // Request 5
			t.Run("Request5", func(t *testing.T) {
				// GetHeight if Ask is returned by requestHandler should not be successful
				server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return Ask }
				request5 := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "GetHeight",
				}

				response5, serverErr, err := testDWSXCall(t, conn, request5)
				assert.NoErrorf(t, err, "Request 5 %q on application %d should not error: %s", request5.Method, i, err)
				assert.NotNil(t, response5, "Response 5 on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 5 on application %d should have error as permission was Ask: %v", i, serverErr)
				assert.Equal(t, PermissionDenied, serverErr.Code, "Response 5 on application %d should be %v: %v", i, PermissionDenied, serverErr.Code)
			})

			// // Request 6
			t.Run("Request6", func(t *testing.T) {
				// Invalid method should fail
				request6 := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "SomeInvalidMethodName", // Invalid method
				}

				response6, serverErr, err := testDWSXCall(t, conn, request6)
				assert.NoErrorf(t, err, "Request 6 %q on application %d should not error: %s", request6.Method, i, err)
				assert.NotNil(t, response6, "Response 6 on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 6 on application %d should have error as method was invalid: %v", i, serverErr)
				assert.Equal(t, code.MethodNotFound, serverErr.Code, "Response 6 on application %d should be %v: %v", i, code.MethodNotFound, serverErr.Code)
			})

			// // Request 7
			t.Run("Request7", func(t *testing.T) {
				assert.Len(t, server.GetApplications(), 1, "Request 7 on application %d there should only be one application present", i)
				// Re send the same application data when already connected
				err = conn.WriteJSON(app)
				assert.NoErrorf(t, err, "Request 7 on application %d failed to resend data to server: %s", i, err)
				reauthResponse := testHandleAuthResponse(t, conn)
				assert.False(t, reauthResponse.Accepted, "Response 7 on application %d should not be re-accepted and has been", i)
				assert.Len(t, server.GetApplications(), 1, "Response 7 on application %d there should only be one application present", i)
			})

			// // Request 8
			t.Run("Request8", func(t *testing.T) {
				// Call a added method
				methodName := HasMethod_Params{Name: "ACustomMethod"}
				server.SetCustomMethod(methodName.Name, func(context.Context, *jrpc2.Request) (interface{}, error) { return nil, nil })

				request8a := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "HasMethod",
					Params:  methodName,
				}

				// Set requestHandler to Allow
				server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return Allow }

				// Call HasMethod on the added method
				response8a, serverErr, err := testDWSXCall(t, conn, request8a)
				assert.NoErrorf(t, err, "Request 8a %q on application %d should not error: %s", request8a.Method, i, err)
				assert.NotNil(t, response8a, "Response 8a on application %d should not be nil", i)
				assert.Nil(t, serverErr, "Response 8a on application %d should not have error: %v", i, serverErr)
				assert.IsType(t, true, response8a.Result, "Response 8a on application %d should be bool: %T", i, response8a.Result)
				assert.True(t, response8a.Result.(bool), "Response 8a on application %d should have method: %s", i, methodName.Name)

				// Call HasMethod on the method not added
				methodName.Name = "NewCutsomMethod"
				request8b := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "HasMethod",
					Params:  methodName,
				}

				response8b, serverErr, err := testDWSXCall(t, conn, request8b)
				assert.NoErrorf(t, err, "Request 8b %q on application %d should not error: %s", request8b.Method, i, err)
				assert.NotNil(t, response8b, "Response 8b on application %d should not be nil", i)
				assert.Nil(t, serverErr, "Response 8b on application %d should not have error: %v", i, serverErr)
				assert.IsType(t, false, response8a.Result, "Response 8b on application %d should be bool: %T", i, response8b.Result)
				assert.False(t, response8b.Result.(bool), "Response 8b on application %d should not have method: %s", i, methodName.Name)
			})

			// Invalid data attempts expected to fail
			expectedErr := code.ParseError

			// // Request 9
			t.Run("Request9", func(t *testing.T) {
				// Invalid json data
				request9 := jsonrpc.RPCRequest{
					JSONRPC: "request",
					ID:      9,
					Method:  "GetAddress",
				}
				response9, serverErr, err := testDWSXCall(t, conn, request9)
				assert.NoErrorf(t, err, "Request 9 %q on application %d should not error: %s", request9.Method, i, err)
				assert.NotNil(t, response9, "Response 9 on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 9 on application %d should have error: %v", i, serverErr)
				assert.Equal(t, expectedErr, serverErr.Code, "Response 9 on application %d should be %v: %v", i, expectedErr, serverErr.Code)
			})

			// // Request 10
			t.Run("Request10", func(t *testing.T) {
				// Invalid data
				request10 := rpc.Transfer_Params{
					Transfers: []rpc.Transfer{},
					SC_Code:   "DERO",
					SC_Value:  0,
					SC_ID:     "DERO",
					SC_RPC:    []rpc.Argument{},
					Ringsize:  0,
					Fees:      0,
					Signer:    "DERO",
				}
				response10, serverErr, err := testDWSXCall(t, conn, request10)
				assert.NoErrorf(t, err, "Request 10 on application %d should not error: %s", i, err)
				assert.NotNil(t, response10, "Result 10 on application %d should not be nil", i)
				assert.Error(t, serverErr, "Result 10 on application %d should have error: %v", i, serverErr)
				assert.Equal(t, expectedErr, serverErr.Code, "Result 10 on application %d should be %v: %v", i, expectedErr, serverErr.Code)
			})

			// // Request 11
			t.Run("Request11", func(t *testing.T) {
				// More invalid data
				request11 := "thisisainvalidrequest"
				response11, serverErr, err := testDWSXCall(t, conn, request11)
				assert.NoErrorf(t, err, "Request 11 on application %d should not error: %s", i, err)
				assert.NotNil(t, response11, "Result 11 on application %d should not be nil", i)
				assert.Error(t, serverErr, "Result 11 on application %d should have error: %v", i, serverErr)
				assert.Equal(t, expectedErr, serverErr.Code, "Result 11 on application %d should be %v: %v", i, expectedErr, serverErr.Code)
			})

			// // Request 12
			t.Run("Request12", func(t *testing.T) {
				// Subscribe and simulate broadcasting event
				var result12 rpc.EventNotification
				params12 := Subscribe_Params{Event: rpc.NewTopoheight}
				request12a := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "Subscribe",
					Params:  params12,
				}
				response12a, serverErr, err := testDWSXCall(t, conn, request12a)
				assert.NoErrorf(t, err, "Request 12a on application %d should not error: %s", i, err)
				assert.NotNil(t, response12a, "Response 12a on application %d should not be nil", i)
				assert.Nil(t, serverErr, "Response 12a on application %d should not have error: %v", i, serverErr)

				// Test broadcasting event
				broadcast := float64(600)
				assert.True(t, server.IsEventTracked(rpc.NewTopoheight), "Event should be tracked")
				server.BroadcastEvent(rpc.NewTopoheight, broadcast)
				time.Sleep(10 * time.Millisecond)

				// Test reading the event
				_, message, err := conn.ReadMessage()
				assert.NoErrorf(t, err, "Read 12 on application %d should not error: %s", i, err)
				assert.NotNil(t, message, "Message 12 on application %d should not be nil", i)

				var event12 RPCResponse
				err = json.Unmarshal(message, &event12)
				assert.NoErrorf(t, err, "Unmarshal 12 on application %d should not error: %s", i, err)
				js, err := json.Marshal(event12.Result)
				assert.NoErrorf(t, err, "Marshal 12 on application %d should not error: %s", i, err)
				err = json.Unmarshal(js, &result12)
				assert.NoErrorf(t, err, "Unmarshal 12 on application %d should not error: %s", i, err)
				assert.Equal(t, broadcast, result12.Value, "Broadcast value on application %d is not equal: %v", i, result12.Value)

				// Unsubscribe to tracked event
				request12b := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "Unsubscribe",
					Params:  params12,
				}
				response12b, serverErr, err := testDWSXCall(t, conn, request12b)
				assert.NoErrorf(t, err, "Request 12b on application %d should not error: %s", i, err)
				assert.NotNil(t, response12b, "Response 12b on application %d should not be nil", i)
				assert.Nil(t, serverErr, "Response 12b on application %d should not have error: %v", i, serverErr)
				assert.False(t, server.IsEventTracked(params12.Event), "Event on application %d should not be tracked after %q", i, request12b.Method)
			})

			// // Request 13 request
			t.Run("Request13", func(t *testing.T) {
				somedata := []byte(app.Id)
				// SignData should return successfully as requestHandler is Allow
				request13a := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "SignData",
					Params:  somedata,
				}
				response13a, serverErr, err := testDWSXCall(t, conn, request13a)
				assert.NoErrorf(t, err, "Request 13a %q on application %d should not error: %s", request13a.Method, i, err)
				assert.NotNil(t, response13a, "Response 13a on application %d should not be nil", i)
				assert.Nil(t, serverErr, "Response 13a on application %d should not have error: %v", i, serverErr)
				assert.NotNil(t, response13a.Result, "Response 13a on application %d should not be nil", i)

				// Test signature message matches somedata
				assert.IsType(t, "string", response13a.Result, "Response 13a on application %d should be string: %T", i, response13a.Result)
				decodeString, err := base64.StdEncoding.DecodeString(response13a.Result.(string))
				assert.NoErrorf(t, err, "Decode 13 on application %d should not error: %s", i, err)
				signer, message, err := server.wallet.CheckSignature(decodeString)
				assert.NoErrorf(t, err, "Reading signature on application %d should not error: %s", i, err)
				assert.Equal(t, testWalletData[0].Address, signer.String(), "Signers walletapi %d does not match %s: %s", i, testWalletData[0].Address, signer.String())
				assert.Equal(t, somedata, message, "Signed walletapi messages %d do not match %s: %s", i, somedata, message)

				// Test DWSX CheckSignature result matches walletapi results
				var result13b Signature_Result
				request13b := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "CheckSignature",
					Params:  decodeString,
				}
				response13b, serverErr, err := testDWSXCall(t, conn, request13b)
				assert.NoErrorf(t, err, "Request 13b %q on application %d should not error: %s", request13b.Method, i, err)
				assert.NotNil(t, response13b, "Response 13b on application %d should not be nil", i)
				assert.Nil(t, serverErr, "Response 13b on application %d should not have error: %v", i, serverErr)
				assert.NotNil(t, response13b.Result, "Response 13b on application %d should not be nil", i)

				js, err := json.Marshal(response13b.Result)
				assert.NoErrorf(t, err, "Request 13b marshal on application %d should not error: %s", i, err)
				err = json.Unmarshal(js, &result13b)
				assert.NoErrorf(t, err, "Request 13b unmarshal on application %d should not error: %s", i, err)
				assert.Equal(t, testWalletData[0].Address, result13b.Signer, "Signers %q %d does not match %s: %s", request13b.Method, i, testWalletData[0].Address, signer.String())
				assert.Equal(t, string(message), result13b.Message, "Signed %q messages %d do not match %s: %s", request13b.Method, i, somedata, result13b.Message)

				// Test CheckSignature with invalid signature
				request13b.Params = []byte("not a valid signature")
				response13c, serverErr, err := testDWSXCall(t, conn, request13b)
				assert.NoErrorf(t, err, "Request 13c %q on application %d should not error: %s", request13b.Method, i, err)
				assert.NotNil(t, response13c, "Response 13c on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 13c on application %d should have error: %v", i, serverErr)
				assert.Equal(t, code.InternalError, serverErr.Code, "Response 13c on application %d should be %v: %v", i, code.InternalError, serverErr.Code)

				// Test SignData again with Deny permission
				server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return Deny }

				response13d, serverErr, err := testDWSXCall(t, conn, request13a)
				assert.NoErrorf(t, err, "Request 13d %q on application %d should not error: %s", request13a.Method, i, err)
				assert.NotNil(t, response13d, "Response 13d on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 13d on application %d should have error as permission was Deny: %v", i, serverErr)
				assert.Equal(t, PermissionDenied, serverErr.Code, "Response 13d on application %d should be %v: %v", i, PermissionDenied, serverErr.Code)
			})

			// // Request 14
			t.Run("Request14", func(t *testing.T) {
				// Allow this request
				server.requestHandler = func(ad *ApplicationData, r *jrpc2.Request) Permission { return Allow }
				// Call DWSX GetDaemon expecting to fail as daemon is not connected
				request14 := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "GetDaemon",
				}
				response14a, serverErr, err := testDWSXCall(t, conn, request14)
				assert.NoErrorf(t, err, "Request 14a %q on application %d should not error: %s", request14.Method, i, err)
				assert.NotNil(t, response14a, "Response 14a on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 14a on application %d should have error: %v", i, serverErr)
				assert.Equal(t, code.InternalError, serverErr.Code, "Response 14a on application %d should be %v: %v", i, code.InternalError, serverErr.Code)

				// Call again with Deny should fail
				server.requestHandler = func(ad *ApplicationData, r *jrpc2.Request) Permission { return Deny }
				response14b, serverErr, err := testDWSXCall(t, conn, request14)
				assert.NoErrorf(t, err, "Request 14b %q on application %d should not error: %s", request14.Method, i, err)
				assert.NotNil(t, response14b, "Response 14b on application %d should not be nil", i)
				assert.Error(t, serverErr, "Response 14b on application %d should have error: %v", i, serverErr)
				assert.Equal(t, PermissionDenied, serverErr.Code, "Response 14b on application %d should be %v: %v", i, PermissionDenied, serverErr.Code)
			})

			// Close the app connection
			conn.Close()
			time.Sleep(10 * time.Millisecond)

			// Reset requestHandler to Allow before beginning next connection
			server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return Allow }

			// Ensure there is no apps as connection was closed
			assert.Len(t, server.GetApplications(), 0, "There should be no applications")
		}
	})

	// Tests invalid ApplicationData and double adding a application
	t.Run("ApplicationData", func(t *testing.T) {
		assert.Len(t, server.GetApplications(), 0, "There should be no applications")
		// Simulate user accepting the application connection request
		server.appHandler = func(ad *ApplicationData) bool { return true }

		// Simulate Allow permission request
		server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return Allow }

		conn, err := testCreateClient(nil)
		assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
		t.Cleanup(func() { conn.Close() })

		request1 := jsonrpc.RPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "DERO.Ping",
		}

		expected := "Invalid app data format"

		err = conn.WriteJSON(request1)
		assert.NoErrorf(t, err, "Request 1 %s should not error: %s", request1.Method, err)
		authResponse := testHandleAuthResponse(t, conn)
		assert.False(t, authResponse.Accepted, "Response 1 application should not be accepted and is")
		assert.Equal(t, expected, authResponse.Message, "Response 1 application error message not equal: %s", authResponse.Message)

		// Reset connection and try to add the same application twice
		conn.Close()
		conn, err = testCreateClient(nil)
		assert.NoErrorf(t, err, "Application failed to dial server: %s", err)

		app := testAppData[2]

		// Add application once
		err = conn.WriteJSON(app)
		assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
		authResponse = testHandleAuthResponse(t, conn)
		assert.True(t, authResponse.Accepted, "Application should be accepted and is not")

		// Second instance of same application tries to add it itself again
		double, err := testCreateClient(nil)
		assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
		expected = "App ID is already used"
		err = double.WriteJSON(app)
		assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
		authResponse = testHandleAuthResponse(t, double)
		assert.False(t, authResponse.Accepted, "Application should not be accepted twice and is")
		assert.Equal(t, expected, authResponse.Message, "Server response should be equal: %s", authResponse.Message)

		// Create test client with invalid origin
		header := http.Header{}
		header.Set("Origin", "http://invalidtestorigin.com")
		double, err = testCreateClient(header)
		assert.NoErrorf(t, err, "Application %d failed to dial server: %s", err)
		defer double.Close()
		err = double.WriteJSON(testAppData[0])
		assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
		authResponse = testHandleAuthResponse(t, double)
		assert.False(t, authResponse.Accepted, "Application should not be accepted and is")
		time.Sleep(10 * time.Millisecond)
	})

	// Test adding multiple applications
	t.Run("MultipleApplications", func(t *testing.T) {
		// Simulate user accepting the application connection request
		server.appHandler = func(ad *ApplicationData) bool { return true }
		// No requests used
		server.requestHandler = func(app *ApplicationData, request *jrpc2.Request) Permission { return Allow }

		for i, app := range testAppData {
			conn, err := testCreateClient(nil)
			assert.NoErrorf(t, err, "Application %d failed to dial server: %s", i, err)
			t.Cleanup(func() { conn.Close() })

			err = conn.WriteJSON(app)
			assert.NoErrorf(t, err, "Application %d failed to write data to server: %s", i, err)
			authResponse := testHandleAuthResponse(t, conn)
			if i < validTo {
				assert.True(t, authResponse.Accepted, "Application %d should be accepted and is not", i)
			} else {
				assert.False(t, authResponse.Accepted, "Application %s should not be accepted and is", i)
			}
		}

		// Test the added ApplicationData
		apps := server.GetApplications()
		assert.Len(t, apps, validTo, "Should have 7 apps")
		for i := 0; i < validTo; i++ {
			assert.True(t, server.HasApplicationId(testAppData[i].Id), "Application %d data is missing", i)
		}

		// Test removing the added ApplicationData
		assert.Len(t, apps, validTo, "Should have 7 apps")
		for i := 0; i < validTo; i++ {
			server.RemoveApplication(&apps[i])
			assert.Len(t, server.GetApplications(), validTo-(i+1), "Application %d should have been removed", i)
		}
	})

	// Test sending multiple concurrent requests
	t.Run("Concurrent", func(t *testing.T) {
		assert.Len(t, server.GetApplications(), 0, "Application should not be present and is")
		server.appHandler = func(ad *ApplicationData) bool { return true }
		// Give some time between allowing requests
		server.requestHandler = func(ad *ApplicationData, r *jrpc2.Request) Permission {
			time.Sleep(time.Millisecond * 5)
			return Allow
		}

		// Wait for routines to complete all requests
		var wg sync.WaitGroup
		wg.Add(5)
		requests := 300

		// // Request 1
		go func() {
			defer wg.Done()
			t.Run("Request1", func(t *testing.T) {
				conn1, err := testCreateClient(nil)
				assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
				defer conn1.Close()
				err = conn1.WriteJSON(testAppData[0])
				assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
				authResponse := testHandleAuthResponse(t, conn1)
				assert.True(t, authResponse.Accepted, "Application should be accepted and is not")
				// GetAddress should succeed
				request1 := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "GetAddress",
				}

				for i := 0; i < requests; i++ {
					response1, serverErr, err := testDWSXCall(t, conn1, request1)
					assert.NoErrorf(t, err, "Request 1 %q should not give error: %s", request1.Method, err)
					assert.NotNil(t, response1, "Response 1 should not be nil")
					assert.Nil(t, serverErr, "Response 1 should not have error: %v", serverErr)
					assert.IsType(t, map[string]interface{}{}, response1.Result, "Response 1 should be map[string]interface{}: %T", response1.Result)
					assert.Equal(t, testWalletData[0].Address, response1.Result.(map[string]interface{})["address"].(string))
				}
			})
		}()

		// // Request 2
		go func() {
			defer wg.Done()
			t.Run("Request2", func(t *testing.T) {
				conn2, err := testCreateClient(nil)
				assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
				defer conn2.Close()
				err = conn2.WriteJSON(testAppData[1])
				assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
				authResponse := testHandleAuthResponse(t, conn2)
				assert.True(t, authResponse.Accepted, "Application should be accepted and is not")
				// GetHeight should succeed
				var result2 rpc.GetHeight_Result
				request2 := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "GetHeight",
				}

				for i := 0; i < requests; i++ {
					response2, serverErr, err := testDWSXCall(t, conn2, request2)
					assert.NoErrorf(t, err, "Request 2 %q should not give error: %s", request2.Method, err)
					assert.NotNil(t, response2, "Response 2 should not be nil")
					assert.Nil(t, serverErr, "Response 2 should not have error: %v", serverErr)
					js, err := json.Marshal(response2.Result)
					assert.NoErrorf(t, err, "Request 2 marshal should not give error: %s", request2.Method, err)
					err = json.Unmarshal(js, &result2)
					assert.NoErrorf(t, err, "Request 2 unmarshal should not give error: %s", request2.Method, err)
				}
			})
		}()

		// // Request 3
		go func() {
			defer wg.Done()
			t.Run("Request3", func(t *testing.T) {
				conn3, err := testCreateClient(nil)
				assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
				defer conn3.Close()
				err = conn3.WriteJSON(testAppData[2])
				assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
				authResponse := testHandleAuthResponse(t, conn3)
				assert.True(t, authResponse.Accepted, "Application should be accepted and is not")
				// Echo should succeed
				params3 := []string{"DERO", "Test", "DWSX"}
				request3 := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "Echo",
					Params:  params3,
				}

				for i := 0; i < requests; i++ {
					response3, serverErr, err := testDWSXCall(t, conn3, request3)
					assert.NoErrorf(t, err, "Request 3 %q should not error: %s", request3.Method, err)
					assert.NotNil(t, response3, "Response 3 should not be nil")
					assert.Nil(t, serverErr, "Response 3 should not have error: %v", serverErr)
					assert.Equal(t, fmt.Sprintf("WALLET %s", strings.Join(params3, " ")), response3.Result, "Response 3 does not match expected: %v", response3.Result)
				}
			})
		}()

		// Test concurrent event subscriptions

		// // Request 4
		go func() {
			defer wg.Done()
			t.Run("Request4", func(t *testing.T) {
				conn4, err := testCreateClient(nil)
				assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
				defer conn4.Close()
				err = conn4.WriteJSON(testAppData[3])
				assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
				authResponse := testHandleAuthResponse(t, conn4)
				assert.True(t, authResponse.Accepted, "Application should be accepted and is not")

				// Subscribe and simulate broadcasting event
				subscribe := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "Subscribe",
					Params:  Subscribe_Params{Event: rpc.NewTopoheight},
				}
				response4, serverErr, err := testDWSXCall(t, conn4, subscribe)
				assert.NoErrorf(t, err, "Request 4 should not error: %s", err)
				assert.NotNil(t, response4, "Response 4 should not be nil")
				assert.Nil(t, serverErr, "Response 4 should not have error: %v", serverErr)

				// Another app subscribing to same event
				conn6, err := testCreateClient(nil)
				assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
				defer conn6.Close()
				err = conn6.WriteJSON(testAppData[5])
				assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
				authResponse = testHandleAuthResponse(t, conn6)
				assert.True(t, authResponse.Accepted, "Application should be accepted and is not")

				response6, serverErr, err := testDWSXCall(t, conn6, subscribe)
				assert.NoErrorf(t, err, "Request 6 should not error: %s", err)
				assert.NotNil(t, response6, "Response 6 should not be nil")
				assert.Nil(t, serverErr, "Response 6 should not have error: %v", serverErr)

				for i := 0; i < requests; i++ {
					// Test broadcasting event
					broadcast := float64(600 + i)
					assert.True(t, server.IsEventTracked(rpc.NewTopoheight), "Event should be tracked")
					server.BroadcastEvent(rpc.NewTopoheight, broadcast)
					time.Sleep(15 * time.Millisecond)

					// Test reading the event
					_, message, err := conn4.ReadMessage()
					assert.NoErrorf(t, err, "Read 4 should not error: %s", err)
					assert.NotNil(t, message, "Message 4 should not be nil")

					var event4 RPCResponse
					err = json.Unmarshal(message, &event4)
					assert.NoErrorf(t, err, "Unmarshal 4 event should not error: %s", err)
					js, err := json.Marshal(event4.Result)
					assert.NoErrorf(t, err, "Marshal 4 event should not error: %s", err)

					var result4 rpc.EventNotification
					err = json.Unmarshal(js, &result4)
					assert.NoErrorf(t, err, "Unmarshal 4 result should not error: %s", err)
					assert.Equal(t, broadcast, result4.Value, "Broadcast 4 value is not equal: %v", result4.Value)

					// Second app reading the same event
					_, message, err = conn6.ReadMessage()
					assert.NoErrorf(t, err, "Read 6 should not error: %s", err)
					assert.NotNil(t, message, "Message 6 should not be nil")

					var event6 RPCResponse
					err = json.Unmarshal(message, &event6)
					assert.NoErrorf(t, err, "Unmarshal 6 event should not error: %s", err)
					js, err = json.Marshal(event6.Result)
					assert.NoErrorf(t, err, "Marshal 6 event should not error: %s", err)

					var result6 rpc.EventNotification
					err = json.Unmarshal(js, &result6)
					assert.NoErrorf(t, err, "Unmarshal 6 result should not error: %s", err)
					assert.NotNil(t, result6.Value, "Broadcast value should not be nil")
					assert.Equal(t, broadcast, result6.Value, "Broadcast 6 value is not equal: %v", result6.Value)
				}
			})
		}()

		// // Request 5
		go func() {
			defer wg.Done()
			t.Run("Request5", func(t *testing.T) {
				conn5, err := testCreateClient(nil)
				assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
				defer conn5.Close()
				err = conn5.WriteJSON(testAppData[4])
				assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
				authResponse := testHandleAuthResponse(t, conn5)
				assert.True(t, authResponse.Accepted, "Application should be accepted and is not")

				// Subscribe and simulate broadcasting event
				subscribe := jsonrpc.RPCRequest{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "Subscribe",
					Params:  Subscribe_Params{Event: rpc.NewEntry},
				}
				response5, serverErr, err := testDWSXCall(t, conn5, subscribe)
				assert.NoErrorf(t, err, "Request 5 should not error: %s", err)
				assert.NotNil(t, response5, "Response 5 should not be nil")
				assert.Nil(t, serverErr, "Response 5 not have error: %v", serverErr)

				for i := 0; i < requests; i++ {
					// Test broadcasting event
					tx := fmt.Sprintf("%d", i)
					broadcast := rpc.Entry{
						Height:   uint64(i),
						Incoming: true,
						TXID:     fmt.Sprintf("%x", i*i),
						Sender:   tx,
					}
					assert.True(t, server.IsEventTracked(rpc.NewEntry), "Event should be tracked")
					server.BroadcastEvent(rpc.NewEntry, broadcast)
					time.Sleep(10 * time.Millisecond)

					// Test reading the event
					_, message, err := conn5.ReadMessage()
					assert.NoErrorf(t, err, "Read 5 should not error: %s", err)
					assert.NotNil(t, message, "Message 5 should not be nil")

					var event5 RPCResponse
					err = json.Unmarshal(message, &event5)
					assert.NoErrorf(t, err, "Unmarshal 5 event should not error: %s", err)
					js, err := json.Marshal(event5.Result)
					assert.NoErrorf(t, err, "Marshal 5 event should not error: %s", err)

					var result5 rpc.EventNotification
					err = json.Unmarshal(js, &result5)
					assert.NoErrorf(t, err, "Unmarshal 5 result should not error: %s", err)

					// Ensure value is rpc.Entry
					var final5 rpc.Entry
					js, err = json.Marshal(result5.Value)
					assert.NoErrorf(t, err, "Marshal 5 final should not error: %s", err)
					err = json.Unmarshal(js, &final5)
					assert.NoErrorf(t, err, "Unmarshal 5 final should not error: %s", err)
					assert.Equal(t, broadcast, final5, "Broadcast value is not equal: %v", final5)
				}
			})
		}()

		wg.Wait()
	})
}

// TestDWSXServerWithPort tests request with stored permissions and daemon calls
func TestDWSXServerWithPort(t *testing.T) {
	// Simulate user accepting the application connection request
	appHandler := true

	// Deny permission request for these tests, stored permissions
	requestHandler := Deny

	// Using NewDWSXServerWithPort with !forceAsk to allow storing permission requests
	_, server, err := newDWSXServer(t, true, appHandler, requestHandler)
	assert.NoErrorf(t, err, "newDWSXServer should not error: %s", err)
	defer server.Stop()

	// Test that stored permissions are valid
	t.Run("Permissions", func(t *testing.T) {
		// Apps 0-6 will be valid in this case, 1-2 have valid permission requests, 5-6 conflicting requests
		for i, app := range testAppData {
			// Create a websocket client to connect to the server
			conn, err := testCreateClient(nil)
			assert.NoErrorf(t, err, "Application %d failed to dial server: %s", i, err)
			defer conn.Close()

			// Send ApplicationData to server
			err = conn.WriteJSON(app)
			assert.NoErrorf(t, err, "Application %d failed to write data to server: %s", i, err)

			authResponse := testHandleAuthResponse(t, conn)
			t.Logf("Authorization response %d: %v", i, authResponse.Message)
			if i < validTo {
				assert.True(t, authResponse.Accepted, "Application %d should be accepted and is not", i)
				// Was application added to the server
				assert.Len(t, server.GetApplications(), 1, "Application %d should be present and is not", i)
				assert.True(t, server.HasApplicationId(app.Id), "Application ID %d should be present and is not", i)
			} else {
				assert.False(t, authResponse.Accepted, "Application %d should not be accepted and is", i)
				assert.Len(t, server.GetApplications(), 0, "Application %d should not be present and is", i)
			}

			// Check app permissions
			for _, app := range server.GetApplications() {
				validPermissions := map[string]Permission{}
				normalizedMethods := map[string]Permission{}
				for name, p := range app.Permissions {
					// t.Logf("perm %s %s", name, p)
					assert.NotEqual(t, Allow, p, "Allow should not be stored:", name)
					assert.NotEqual(t, Deny, p, "Deny should not be stored:", name)
					assert.Contains(t, server.rpcHandler, name, "%s is not in rpcHandler and is requesting permissions:", name)

					// If two of the same method added make sure they are the same
					normalized := strings.ToLower(strings.ReplaceAll(name, "_", ""))
					if pcheck, ok := normalizedMethods[normalized]; ok {
						// App 2 hits here on GetAddress
						assert.Equal(t, p, pcheck, "Conflicting permissions requested %s %s/%s", name, p, pcheck)
					}

					normalizedMethods[normalized] = p
					validPermissions[name] = p
				}
				if len(validPermissions) > 0 {
					assert.Len(t, validPermissions, 3, "Should have only 3 permission types stored")
					t.Logf("Requested permissions for %s: %v", app.Name, validPermissions)
				}
			}

			// Check if app has been added and remove it
			if i < validTo {
				assert.Len(t, server.GetApplications(), 1, "There should be one application")
				server.RemoveApplication(&app)
			}

			assert.Len(t, server.GetApplications(), 0, "There should be no applications")
		}
	})

	// Send ApplicationData to server with these permissions accepted
	// "GetHeight":    AlwaysDeny,
	// "GetAddress":   AlwaysAllow,
	// "GetTransfers": Allow,
	// "transfer":     Deny,
	// "GetBalance":   Ask,
	app := testAppData[1]

	// Test requests on stored permissions
	t.Run("Stored", func(t *testing.T) {
		// Create a websocket client to connect to the server
		conn, err := testCreateClient(nil)
		assert.NoErrorf(t, err, "Application failed to dial server: %s", err)

		err = conn.WriteJSON(app)
		assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)

		authResponse := testHandleAuthResponse(t, conn)
		t.Logf("Authorization response: %v", authResponse)
		assert.True(t, authResponse.Accepted, "Application should be accepted and is not")

		// Was application added to the server
		assert.Len(t, server.GetApplications(), 1, "Application should be present and is not")
		assert.True(t, server.HasApplicationId(app.Id), "Application ID should be present and is not")

		// // Request 0
		t.Run("Request0", func(t *testing.T) {
			// GetAddress should succeed as it has AlwaysAllow permission set
			request0 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "GetAddress",
			}
			response0, serverErr, err := testDWSXCall(t, conn, request0)
			assert.NoErrorf(t, err, "Request 0 %q should not give error: %s", request0.Method, err)
			assert.NotNil(t, response0, "Response 0 should not be nil")
			assert.Nil(t, serverErr, "Response 0 should not have error: %v", serverErr)
		})

		// // Request 1
		t.Run("Request1", func(t *testing.T) {
			// GetHeight should fail as it has AlwaysDeny permission set
			request1 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "GetHeight",
			}
			response1, serverErr, err := testDWSXCall(t, conn, request1)
			assert.NoErrorf(t, err, "Request 1 %q should not give error: %s", request1.Method, err)
			assert.NotNil(t, response1, "Response 1 should not be nil")
			assert.Error(t, serverErr, "Response 1 should have error: %v", serverErr)
			assert.Equal(t, PermissionAlwaysDenied, serverErr.Code, "Response 1 should be %v: %v", PermissionAlwaysDenied, serverErr.Code)
		})

		// // Request 2
		t.Run("Request2", func(t *testing.T) {
			// GetTransfers should fail as it should not have permission stored
			request2 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "GetTransfers",
			}
			response2, serverErr, err := testDWSXCall(t, conn, request2)
			assert.NoErrorf(t, err, "Request 2 %q should not give error: %s", request2.Method, err)
			assert.NotNil(t, response2, "Response 2 should not be nil")
			assert.Error(t, serverErr, "Response 2 should have error: %v", serverErr)
			assert.Equal(t, PermissionDenied, serverErr.Code, "Response 2 should be %v: %v", PermissionDenied, serverErr.Code)
		})

		// // Request 3
		t.Run("Request3", func(t *testing.T) {
			// transfer should fail as it should not have permission stored
			request3 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "transfer",
			}
			response3, serverErr, err := testDWSXCall(t, conn, request3)
			assert.NoErrorf(t, err, "Request 3 %q should not give error: %s", request3.Method, err)
			assert.NotNil(t, response3, "Response 3 should not be nil")
			assert.Error(t, serverErr, "Response 3 should have error: %v", serverErr)
			assert.Equal(t, PermissionDenied, serverErr.Code, "Response 3 should be %v: %v", PermissionDenied, serverErr.Code)
		})

		// // Request 4
		t.Run("Request4", func(t *testing.T) {
			// GetBalance should fail as it should not have permission stored
			request4 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "GetBalance",
			}
			response4, serverErr, err := testDWSXCall(t, conn, request4)
			assert.NoErrorf(t, err, "Request 4 %q should not give error: %s", request4.Method, err)
			assert.NotNil(t, response4, "Response 4 should not be nil")
			assert.Error(t, serverErr, "Response 4 should have error: %v", serverErr)
			assert.Equal(t, PermissionDenied, serverErr.Code, "Response 4 should be %v: %v", PermissionDenied, serverErr.Code)
		})

		// // Request 5
		t.Run("Request5", func(t *testing.T) {
			// scinvoke should fail as it should not have permission stored
			request5 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "scinvoke",
			}
			response5, serverErr, err := testDWSXCall(t, conn, request5)
			assert.NoErrorf(t, err, "Request 5 %q should not give error: %s", request5.Method, err)
			assert.NotNil(t, response5, "Response 5 should not be nil")
			assert.Error(t, serverErr, "Response 5 should have error: %v", serverErr)
			assert.Equal(t, PermissionDenied, serverErr.Code, "Response 5 should be %v: %v", PermissionDenied, serverErr.Code)
		})

		// // Request 6
		t.Run("Request6", func(t *testing.T) {
			// DERO.Ping should fail daemon is not connected
			request6 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "DERO.Ping",
			}
			response6, serverErr, err := testDWSXCall(t, conn, request6)
			assert.NoErrorf(t, err, "Request 6 %q should not give error: %s", request6.Method, err)
			assert.NotNil(t, response6, "Response 6 should not be nil")
			assert.Error(t, serverErr, "Response 6 should have error: %v", serverErr)
			assert.Equal(t, code.Cancelled, serverErr.Code, "Response 6 should be %v: %v", code.Cancelled, serverErr.Code)
		})

		// // Request 7
		t.Run("Request7", func(t *testing.T) {
			// Batch requests should fail
			request7 := []jsonrpc.RPCRequest{
				{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "GetAddress",
				},
				{
					JSONRPC: "2.0",
					ID:      1,
					Method:  "GetBalance",
				},
			}
			response7, serverErr, err := testDWSXCall(t, conn, request7)
			assert.NoErrorf(t, err, "Request 7 batch should not give error: %s", err)
			assert.NotNil(t, response7, "Response 7 should not be nil")
			assert.Error(t, serverErr, "Response 7 should have error: %v", serverErr)
			assert.Equal(t, code.ParseError, serverErr.Code, "Response 7 should be %v: %v", code.ParseError, serverErr.Code)
		})

		// Close the app connection
		conn.Close()

		// // Request 8
		t.Run("Request8", func(t *testing.T) {
			// This should fail as we are not connected
			request8 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "GetAddress",
			}
			err = conn.WriteJSON(request8)
			assert.Error(t, err, "Request 8 %q should error when disconnected", request8.Method)
		})

		time.Sleep(10 * time.Millisecond)

		// Ensure there is no apps as connection was closed
		assert.Len(t, server.GetApplications(), 0, "There should be no applications")
	})

	// Test daemon calls to DWSX server
	t.Run("Daemon", func(t *testing.T) {
		var endpoint string
		var endpoints = []string{"127.0.0.1:10102", "89.38.99.117:10102", "node.derofoundation.org:11012"}
		for i, ep := range endpoints {
			err := walletapi.Connect(ep)
			if err != nil {
				if i == len(endpoints)-1 {
					t.Skipf("Could not connect to fallback endpoint, skipping daemon tests")
				}

				continue
			}

			endpoint = ep
			break
		}
		t.Logf("Wallet is connected to daemon endpoint: %s", endpoint)

		conn, err := testCreateClient(nil)
		assert.NoErrorf(t, err, "Application failed to dial server: %s", err)

		err = conn.WriteJSON(testAppData[2])
		assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
		authResponse := testHandleAuthResponse(t, conn)
		assert.True(t, authResponse.Accepted, "Application should be accepted and is not")

		// // Request 1
		t.Run("Request1", func(t *testing.T) {
			// Ping daemon
			var result1 string
			request1 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "DERO.Ping",
			}
			response1, _, err := testDWSXCall(t, conn, request1)
			assert.NoErrorf(t, err, "Request 1 %s should not error: %s", request1.Method, err)
			js, err := json.Marshal(response1.Result)
			assert.NoErrorf(t, err, "Response 1 %s marshal should not error: %s", request1.Method, err)
			err = json.Unmarshal(js, &result1)
			assert.NoErrorf(t, err, "Response 1 %s unmarshal should not error: %s", request1.Method, err)
			assert.Equal(t, "Pong ", result1, "Response 1 expecting Pong result: %s", result1)
		})

		// // Request 2
		t.Run("Request2", func(t *testing.T) {
			// Call GetInfo
			var result2 rpc.GetInfo_Result
			request2 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "DERO.GetInfo",
			}
			response2, _, err := testDWSXCall(t, conn, request2)
			assert.NoErrorf(t, err, "Request 2 %s should not error: %s", request2.Method, err)
			js, err := json.Marshal(response2.Result)
			assert.NoErrorf(t, err, "Response 2 %s marshal should not error: %s", request2.Method, err)
			err = json.Unmarshal(js, &result2)
			assert.NoErrorf(t, err, "Response 2 %s unmarshal should not error: %s", request2.Method, err)
			assert.False(t, result2.Testnet, "Response 2 testnet should be false on %s", endpoint)
			assert.Greater(t, result2.Height, int64(0), "Response 2 height should be greater than 0")
			assert.Greater(t, result2.Total_Supply, uint64(0), "Response 2 DERO supply should be greater than 0")
			t.Logf("Version: %s", result2.Version)
		})

		// // Request 3
		t.Run("Request3", func(t *testing.T) {
			// Call GetHeight
			var result3 rpc.GetHeight_Result
			request3 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "DERO.GetHeight",
			}
			response3, _, err := testDWSXCall(t, conn, request3)
			assert.NoErrorf(t, err, "Request 3 %s should not error: %s", request3.Method, err)
			js, err := json.Marshal(response3.Result)
			assert.NoErrorf(t, err, "Response 3 %s marshal should not error: %s", request3.Method, err)
			err = json.Unmarshal(js, &result3)
			assert.NoErrorf(t, err, "Response 3 %s unmarshal should not error: %s", request3.Method, err)
			assert.Greater(t, result3.Height, uint64(0), "Response 3 Height should be greater than 0")
		})

		// // Request 4
		t.Run("Request4", func(t *testing.T) {
			// Call GetRandomAddress
			var result4 rpc.GetRandomAddress_Result
			request4 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "DERO.GetRandomAddress",
			}
			response4, _, err := testDWSXCall(t, conn, request4)
			assert.NoErrorf(t, err, "Request 4 %s should not error: %s", request4.Method, err)
			js, err := json.Marshal(response4.Result)
			assert.NoErrorf(t, err, "Response 4 %s marshal should not error: %s", request4.Method, err)
			err = json.Unmarshal(js, &result4)
			assert.NoErrorf(t, err, "Response 4 %s unmarshal should not error: %s", request4.Method, err)
			assert.NotEmpty(t, result4.Address, "Response 4 random addresses should not be empty")
		})

		// // Request 5
		t.Run("Request5", func(t *testing.T) {
			// Call a invalid daemon method
			request5 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "DERO.MethodNotFound",
			}
			_, serverErr, err := testDWSXCall(t, conn, request5)
			assert.NoErrorf(t, err, "Request 5 %s should not error: %s", request5.Method, err)
			assert.Equal(t, code.InvalidRequest, serverErr.Code, "Response 5 should be %v: %v", code.InvalidRequest, serverErr.Code)
		})

		// // Request 6
		t.Run("Request6", func(t *testing.T) {
			// Allow this request
			server.requestHandler = func(ad *ApplicationData, r *jrpc2.Request) Permission { return Allow }
			// Call DWSX GetDaemon
			request6 := jsonrpc.RPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "GetDaemon",
			}
			response6, serverErr, err := testDWSXCall(t, conn, request6)
			assert.NoErrorf(t, err, "Request 6 %s should not error: %s", request6.Method, err)
			assert.NotNil(t, response6, "Response 6 should not be nil")
			assert.Nil(t, serverErr, "Response 6 should not have error: %v", serverErr)
			assert.IsType(t, "string", response6.Result, "Response 6 should be string: %T", response6.Result)
			assert.Equal(t, endpoint, response6.Result.(string))
		})
	})
}

// Test client closures when awaiting responses
func TestDWSXClosures(t *testing.T) {
	_, server, err := newDWSXServer(t, false, true, Allow)
	assert.NoErrorf(t, err, "newDWSXServer should not error: %s", err)
	t.Cleanup(server.Stop)

	// Close the client while awaiting permission request
	t.Run("Closure1", func(t *testing.T) {
		// Create a websocket client to connect to the server
		conn, err := testCreateClient(nil)
		assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
		defer conn.Close()

		// Simulate a permission request awaiting user input
		server.requestHandler = func(ad *ApplicationData, r *jrpc2.Request) Permission {
			// Close the client while awaiting permission
			conn.Close()
			<-ad.OnClose
			time.Sleep(time.Millisecond * 10)
			return Allow
		}

		// Send ApplicationData to server
		err = conn.WriteJSON(testAppData[0])
		assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
		authResponse := testHandleAuthResponse(t, conn)
		assert.True(t, authResponse.Accepted, "Application should be accepted and is not")
		assert.Len(t, server.applications, 1, "There should be one applications")
		// Send a request to server with delayed response from requestHandler
		request1 := jsonrpc.RPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "GetAddress",
		}
		_, _, err = testDWSXCall(t, conn, request1)
		assert.Errorf(t, err, "Request 1a %s should error: %s", request1.Method, err)
		time.Sleep(time.Millisecond * 10)
		assert.Len(t, server.applications, 0, "There should be no applications")

		// Simulate a Allow permission and call again, but client should be already closed
		server.requestHandler = func(ad *ApplicationData, r *jrpc2.Request) Permission { return Allow }
		_, _, err = testDWSXCall(t, conn, request1)
		assert.Errorf(t, err, "Request 1b %s should error: %s", request1.Method, err)
	})

	// Close the client while awaiting connection request
	t.Run("Closure2", func(t *testing.T) {
		assert.Len(t, server.applications, 0, "There should be no applications")
		// Create a websocket client to connect to the server
		conn, err := testCreateClient(nil)
		assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
		defer conn.Close()
		assert.Len(t, server.applications, 0, "There should be no applications")

		// Simulate a connection request awaiting user input
		server.appHandler = func(ad *ApplicationData) bool {
			time.Sleep(time.Second * 3)
			return true
		}

		// Close the client
		go func() {
			time.Sleep(time.Millisecond * 25)
			conn.Close()
		}()

		// Send ApplicationData to server
		err = conn.WriteJSON(testAppData[1])
		assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
		time.Sleep(time.Millisecond * 10)
		assert.Len(t, server.applications, 0, "There should be no applications")
		// Try to call but client should be closed
		request1 := jsonrpc.RPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "GetAddress",
		}
		_, _, err = testDWSXCall(t, conn, request1)
		assert.Errorf(t, err, "Request 1 %s should error: %s", request1.Method, err)
		// Ensure app has been removed
		assert.Len(t, server.applications, 0, "There should be no applications")
	})

	assert.Len(t, server.applications, 0, "There should be no applications")
}

// Test stopping the server when awaiting responses
func TestDWSXStop(t *testing.T) {
	_, server, err := newDWSXServer(t, false, true, Allow)
	assert.NoErrorf(t, err, "newDWSXServer should not error: %s", err)
	t.Cleanup(server.Stop)

	// Stop the server when awaiting permissions request, app will be removed from deferred x.removeApplicationOfSession in readMessageFromSession
	t.Run("Stop1", func(t *testing.T) {
		// Simulate a permission request awaiting user input
		server.requestHandler = func(ad *ApplicationData, r *jrpc2.Request) Permission {
			time.Sleep(time.Second * 2)
			return Allow
		}
		// Create a websocket client to connect to the server
		conn, err := testCreateClient(nil)
		assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
		defer conn.Close()

		// Send ApplicationData to server
		err = conn.WriteJSON(testAppData[2])
		assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
		authResponse := testHandleAuthResponse(t, conn)
		assert.True(t, authResponse.Accepted, "Application should be accepted and is not")
		assert.Len(t, server.applications, 1, "There should be one applications")
		// Send a request to server with delayed response from requestHandler
		request1 := jsonrpc.RPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "GetAddress",
		}

		// Close the server
		go func() {
			time.Sleep(time.Millisecond * 25)
			server.Stop()
		}()

		_, _, err = testDWSXCall(t, conn, request1)
		assert.Errorf(t, err, "Request 1 %s should error: %s", request1.Method, err)
		// Ensure app has been removed
		assert.Len(t, server.applications, 0, "There should be no applications")
	})

	// Stop the server when awaiting connection request,
	// if server is stopped without killing program lifecycle app will not be added even if appHandler can return
	t.Run("Stop2", func(t *testing.T) {
		assert.Len(t, server.applications, 0, "There should be no applications")
		if !server.IsRunning() {
			_, server, err = newDWSXServer(t, false, true, Allow)
			assert.NoErrorf(t, err, "newDWSXServer should not error: %s", err)
		}

		// Simulate a connection request awaiting user input
		server.appHandler = func(ad *ApplicationData) bool {
			time.Sleep(time.Second * 2)
			return true
		}

		// Create a websocket client to connect to the server
		conn, err := testCreateClient(nil)
		assert.NoErrorf(t, err, "Application failed to dial server: %s", err)
		defer conn.Close()

		// Close the server
		go func() {
			time.Sleep(time.Millisecond * 25)
			server.Stop()
		}()

		// Send ApplicationData to server with delayed response from appHandler
		err = conn.WriteJSON(testAppData[3])
		assert.NoErrorf(t, err, "Application failed to write data to server: %s", err)
		// Ensure app has been removed after appHandler returns
		time.Sleep(time.Second * 3)
		_, _, err = conn.ReadMessage()
		assert.Error(t, err, "Application should not be connected")
		assert.Len(t, server.applications, 0, "There should be no applications")
	})

	assert.Len(t, server.applications, 0, "There should be no applications")
}

// Create a testnet wallet and start DWSX server for tests
// If port, server will use NewDWSXServerWithPort w/ !forceAsk, otherwise will use NewDWSXServer
// Simulate initial appHandler and requestHandler values
func newDWSXServer(t *testing.T, port, aHandler bool, rHandler Permission) (dwsxWallet *walletapi.Wallet_Disk, server *DWSX, err error) {
	dwsxWallet, err = walletapi.
		Create_Encrypted_Wallet_From_Recovery_Words(
			"dwsx_text_wallet.db",
			"dwsx",
			testWalletData[0].seed,
		)

	if err != nil {
		return
	}

	// Simulate user accepting or denying the application connection request
	appHandler := func(app *ApplicationData) bool { return aHandler }

	// Simulate user permission when requestHandler is called
	requestHandler := func(app *ApplicationData, request *jrpc2.Request) Permission { return rHandler }

	if port {
		// NewDWSXServerWithPort will use !forceAsk to allow permission requests
		server = NewDWSXServerWithPort(DWSX_PORT, dwsxWallet, false, appHandler, requestHandler)
		t.Logf("Starting NewDWSXServerWithPort: [port: %d, appHandler: %t, requestHandler: %s]", DWSX_PORT, aHandler, rHandler.String())

	} else {
		// NewDWSXServer defaults all permissions to Ask
		server = NewDWSXServer(dwsxWallet, appHandler, requestHandler)
		t.Logf("Starting NewDWSXServer: [appHandler: %t, requestHandler: %s]", aHandler, rHandler.String())
	}

	// Wait for the server to start
	time.Sleep(time.Second)

	if !server.IsRunning() {
		return nil, nil, fmt.Errorf("server is not running and should be")
	}

	return
}

// Create client for DWSX server tests
func testCreateClient(headers http.Header) (conn *websocket.Conn, err error) {
	u := url.URL{Scheme: "ws", Host: "127.0.0.1:44326", Path: "/dwsx"}
	conn, _, err = websocket.DefaultDialer.Dial(u.String(), headers)
	return conn, err
}

// Handle DWSX authentication response for tests
func testHandleAuthResponse(t *testing.T, conn *websocket.Conn) (response AuthorizationResponse) {
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to receive authorization response: %s", err)
	}

	err = json.Unmarshal(message, &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal authorization response: %s", err)
	}

	return
}

// Call and read test requests to DWSX server
func testDWSXCall(t *testing.T, conn *websocket.Conn, request interface{}) (response RPCResponse, jrpcErr *jrpc2.Error, err error) {
	method := "unknown"
	switch r := request.(type) {
	case jsonrpc.RPCRequest:
		method = r.Method
	}

	err = conn.WriteJSON(request)
	if err != nil {
		err = fmt.Errorf("failed to write %s request: %s", method, err)
		return
	}

	_, message, err := conn.ReadMessage()
	if err != nil {
		err = fmt.Errorf("failed to receive %s response: %s", method, err)
		return
	}

	err = json.Unmarshal(message, &response)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal %s response: %s", method, err)
		return
	}
	// t.Logf("%s response: %v", method, response)

	// Parse server response error
	var result []byte
	result, err = json.Marshal(response.Error)
	if err != nil {
		err = fmt.Errorf("could not marshal error result: %s", err)
		return
	}

	err = json.Unmarshal(result, &jrpcErr)
	if err != nil {
		err = fmt.Errorf("could not unmarshal error result to jrpc2.Error: %s", err)
	}

	return
}
