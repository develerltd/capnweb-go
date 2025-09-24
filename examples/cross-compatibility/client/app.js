// Simplified demo without external dependencies
// In a real implementation, this would import from 'capnweb'

class CapnWebDemo {
    constructor() {
        this.api = null;
        this.websocket = null;
        this.setupUI();
    }

    setupUI() {
        // Get DOM elements
        this.connectBtn = document.getElementById('connect');
        this.disconnectBtn = document.getElementById('disconnect');
        this.status = document.getElementById('status');
        this.output = document.getElementById('output');

        // Test buttons
        this.helloBtn = document.getElementById('hello');
        this.timeBtn = document.getElementById('time');
        this.calcBtn = document.getElementById('calc');
        this.echoBtn = document.getElementById('echo');
        this.userBtn = document.getElementById('user');

        // Event listeners
        this.connectBtn.addEventListener('click', () => this.connect());
        this.disconnectBtn.addEventListener('click', () => this.disconnect());

        this.helloBtn.addEventListener('click', () => this.testHello());
        this.timeBtn.addEventListener('click', () => this.testTime());
        this.calcBtn.addEventListener('click', () => this.testCalculate());
        this.echoBtn.addEventListener('click', () => this.testEcho());
        this.userBtn.addEventListener('click', () => this.testUserData());

        this.log("🎯 Cap'n Web JavaScript Client Ready!");
        this.log("📋 This demo shows the client structure for Cap'n Web RPC");
        this.log("Click 'Connect' to establish WebSocket connection to Go backend");
    }

    async connect() {
        try {
            this.log("🔄 Connecting to Go backend...");
            this.setStatus("connecting", "🔄 Connecting...");

            // Create raw WebSocket connection
            this.websocket = new WebSocket("ws://localhost:8081/api");

            this.websocket.onopen = () => {
                this.log("✅ WebSocket connected to Go backend");
                this.log("📝 In a full implementation, this would:");
                this.log("   1. Initialize Cap'n Web RPC session over WebSocket");
                this.log("   2. Exchange capability references");
                this.log("   3. Enable bidirectional method calling");

                this.setStatus("connected", "✅ Connected (Demo Mode)");

                // Enable test buttons
                this.enableTestButtons(true);
                this.connectBtn.disabled = true;
                this.disconnectBtn.disabled = false;
            };

            this.websocket.onmessage = (event) => {
                this.log(`📥 Received: ${event.data}`);
            };

            this.websocket.onerror = (error) => {
                this.log(`❌ WebSocket error: ${error}`);
                this.setStatus("error", "❌ Connection Error");
            };

            this.websocket.onclose = () => {
                this.log("🔌 WebSocket connection closed");
                this.setStatus("disconnected", "⭕ Disconnected");
                this.enableTestButtons(false);
                this.connectBtn.disabled = false;
                this.disconnectBtn.disabled = true;
            };

        } catch (error) {
            this.log(`❌ Connection failed: ${error.message}`);
            this.setStatus("error", "❌ Connection Failed");
        }
    }

    disconnect() {
        if (this.websocket) {
            this.websocket.close();
            this.websocket = null;
        }

        this.log("🔌 Disconnected from Go backend");
        this.setStatus("disconnected", "⭕ Disconnected");

        // Disable test buttons
        this.enableTestButtons(false);
        this.connectBtn.disabled = false;
        this.disconnectBtn.disabled = true;
    }

    testHello() {
        this.log("📤 Demo: Would call Go: api.Hello('JavaScript')");
        this.log("📋 Expected Go response: 'Hello from Go backend, JavaScript! 🎉'");
        this.log("🚧 Requires Cap'n Web RPC message serialization");

        if (this.websocket) {
            // Send demo message showing RPC structure
            const rpcMessage = ["push", {
                type: "call",
                method: "Hello",
                args: ["JavaScript"]
            }];
            this.log(`📤 Would send RPC message: ${JSON.stringify(rpcMessage)}`);
        }
    }

    testTime() {
        this.log("📤 Demo: Would call Go: api.GetTime()");
        this.log("📋 Expected Go response: time.Time serialized as ['date', milliseconds]");
        this.log("🚧 Requires Cap'n Web date serialization");

        if (this.websocket) {
            const rpcMessage = ["push", {
                type: "call",
                method: "GetTime",
                args: []
            }];
            this.log(`📤 Would send RPC message: ${JSON.stringify(rpcMessage)}`);
        }
    }

    testCalculate() {
        this.log("📤 Demo: Would call Go: api.Calculate('multiply', 6, 7)");
        this.log("📋 Expected Go response: 42");
        this.log("📤 Demo: Would call Go: api.Calculate('divide', 10, 0)");
        this.log("📋 Expected Go error: 'division by zero'");
        this.log("🚧 Requires Cap'n Web error handling");

        if (this.websocket) {
            const rpcMessage = ["push", {
                type: "call",
                method: "Calculate",
                args: ["multiply", 6, 7]
            }];
            this.log(`📤 Would send RPC message: ${JSON.stringify(rpcMessage)}`);
        }
    }

    testEcho() {
        this.log("📤 Demo: Would call Go: api.Echo() with JavaScript callback");
        this.log("📋 This demonstrates bidirectional RPC:");
        this.log("   1. JS calls Go.Echo(message, callback)");
        this.log("   2. Go calls the JS callback function");
        this.log("   3. JS callback returns response to Go");
        this.log("   4. Go returns final result to JS");
        this.log("🚧 Requires Cap'n Web capability passing");

        if (this.websocket) {
            const rpcMessage = ["push", {
                type: "call",
                method: "Echo",
                args: ["Hello from JavaScript!", "callback_stub_id"]
            }];
            this.log(`📤 Would send RPC message: ${JSON.stringify(rpcMessage)}`);
        }
    }

    testUserData() {
        this.log("📤 Demo: Would call Go: api.GetUserData(123)");
        this.log("📋 Expected Go response: Complex object with:");
        this.log("   - Numbers, strings, booleans");
        this.log("   - Dates serialized as ['date', milliseconds]");
        this.log("   - Bytes serialized as ['bytes', 'base64']");
        this.log("🚧 Requires Cap'n Web complex type serialization");

        if (this.websocket) {
            const rpcMessage = ["push", {
                type: "call",
                method: "GetUserData",
                args: [123]
            }];
            this.log(`📤 Would send RPC message: ${JSON.stringify(rpcMessage)}`);
        }
    }

    setStatus(type, message) {
        this.status.className = `status ${type}`;
        this.status.textContent = message;
    }

    enableTestButtons(enabled) {
        this.helloBtn.disabled = !enabled;
        this.timeBtn.disabled = !enabled;
        this.calcBtn.disabled = !enabled;
        this.echoBtn.disabled = !enabled;
        this.userBtn.disabled = !enabled;
    }

    log(message) {
        const timestamp = new Date().toLocaleTimeString();
        const logLine = document.createElement('div');
        logLine.innerHTML = `<span class="timestamp">${timestamp}</span> ${message}`;
        this.output.appendChild(logLine);
        this.output.scrollTop = this.output.scrollHeight;

        // Also log to console for debugging
        console.log(`[${timestamp}] ${message}`);
    }
}

// Start the demo when page loads
document.addEventListener('DOMContentLoaded', () => {
    new CapnWebDemo();
});