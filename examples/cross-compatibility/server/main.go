package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// ChatAPI defines our RPC interface that will be called from JavaScript
type ChatAPI struct{}

// Hello returns a greeting message - simple method for basic testing
func (c *ChatAPI) Hello(name string) string {
	return fmt.Sprintf("Hello from Go backend, %s! 🎉", name)
}

// GetTime returns current server time - demonstrates date serialization
func (c *ChatAPI) GetTime() time.Time {
	return time.Now()
}

// Calculate performs arithmetic - demonstrates numeric operations
func (c *ChatAPI) Calculate(operation string, a, b float64) (float64, error) {
	switch operation {
	case "add":
		return a + b, nil
	case "multiply":
		return a * b, nil
	case "divide":
		if b == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return a / b, nil
	default:
		return 0, fmt.Errorf("unknown operation: %s", operation)
	}
}

// Echo demonstrates bidirectional communication - server calls back to client
func (c *ChatAPI) Echo(message string, callback func(string) string) (string, error) {
	// Call the JavaScript callback function
	response := callback(fmt.Sprintf("Server received: %s", message))
	return fmt.Sprintf("Client replied: %s", response), nil
}

// GetUserData demonstrates object passing
func (c *ChatAPI) GetUserData(userID int) map[string]interface{} {
	return map[string]interface{}{
		"id":       userID,
		"name":     fmt.Sprintf("User_%d", userID),
		"email":    fmt.Sprintf("user%d@example.com", userID),
		"created":  time.Now().AddDate(-1, 0, 0), // 1 year ago
		"active":   true,
		"metadata": []byte{0x48, 0x65, 0x6c, 0x6c, 0x6f}, // "Hello" in bytes
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for demo - in production, be more restrictive
		return true
	},
}

func main() {
	fmt.Println("🚀 Cap'n Web Go Backend Server Starting...")
	fmt.Println("==========================================")

	// Create our API implementation
	api := &ChatAPI{}

	// Handle WebSocket connections
	http.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		// Upgrade to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		fmt.Printf("✅ New WebSocket connection from %s\n", r.RemoteAddr)

		// For now, just demonstrate the API structure
		// In a complete implementation, this would:
		// 1. Create Cap'n Web WebSocket transport from conn
		// 2. Create RPC session
		// 3. Export the API object
		// 4. Handle RPC messages

		fmt.Printf("📡 API ready: %+v\n", api)
		fmt.Println("🔍 This is a demo showing the Go backend structure")
		fmt.Println("💡 Full Cap'n Web integration requires implementing:")
		fmt.Println("   - WebSocket message handling")
		fmt.Println("   - RPC session management")
		fmt.Println("   - Method dispatch to API object")

		// Keep connection alive for demo
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				fmt.Println("🔚 Connection closed")
				break
			}
		}
	})

	// Serve static files (HTML, JS, etc.)
	http.Handle("/", http.FileServer(http.Dir("../client/")))

	fmt.Println("🌐 Server running at:")
	fmt.Println("   WebSocket API: ws://localhost:8081/api")
	fmt.Println("   Static files:  http://localhost:8081/")
	fmt.Println()
	fmt.Println("💡 Open http://localhost:8081/ in your browser to test!")
	fmt.Println()
	fmt.Println("📝 Note: This demo shows the Go backend structure.")
	fmt.Println("   Full RPC integration requires completing the Cap'n Web")
	fmt.Println("   WebSocket transport and session implementation.")
	fmt.Println()

	log.Fatal(http.ListenAndServe(":8081", nil))
}