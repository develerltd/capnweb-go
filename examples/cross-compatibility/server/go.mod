module capnweb-demo-server

go 1.24.2

require (
	github.com/cloudflare/capnweb v0.1.0
	github.com/gorilla/websocket v1.5.0
)

replace github.com/cloudflare/capnweb => ../../..
