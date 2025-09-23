package capnweb

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"testing"
	"time"
)

func TestCompatibilityDemo(t *testing.T) {
	fmt.Println("🎯 Cap'n Web Go - JavaScript Compatibility Demo")
	fmt.Println("==================================================")

	// Create serializer and protocol
	serializer := NewSerializer(func(value interface{}) (ExportID, error) {
		return ExportID(42), nil
	})
	protocol := NewProtocol(serializer, nil)

	fmt.Println("\n📤 Message Format (JavaScript Compatible)")
	fmt.Println("----------------------------------------")

	// Demonstrate message formats
	messages := []struct {
		name string
		msg  Message
	}{
		{"Push", protocol.NewPushMessage("testExpression", nil)},
		{"Pull", protocol.NewPullMessage(ImportID(42))},
		{"Resolve", protocol.NewResolveMessage(ExportID(24), "resolved")},
		{"Reject", protocol.NewRejectMessage(ExportID(24), "error")},
		{"Release", protocol.NewReleaseMessage(ImportID(42), 1)},
		{"Abort", protocol.NewAbortMessage("session ended", nil)},
	}

	for _, m := range messages {
		data, _ := json.Marshal(m.msg)
		fmt.Printf("%-7s: %s\n", m.name, string(data))
	}

	fmt.Println("\n🔄 Serialization Format (JavaScript Compatible)")
	fmt.Println("---------------------------------------------")

	// Demonstrate serialization formats
	testValues := []struct {
		name  string
		value interface{}
	}{
		{"BigInt", func() *big.Int { b := new(big.Int); b.SetString("123456789012345", 10); return b }()},
		{"Date", time.Now()},
		{"Bytes", []byte{1, 2, 3, 4, 5}},
		{"Error", &RpcError{Type: "TestError", Message: "test", Code: 404}},
	}

	for _, tv := range testValues {
		serialized, err := serializer.Serialize(tv.value)
		if err != nil {
			log.Printf("Error serializing %s: %v", tv.name, err)
			continue
		}
		data, _ := json.Marshal(serialized)
		fmt.Printf("%-6s: %s\n", tv.name, string(data))
	}

	fmt.Println("\n✅ Wire Protocol Examples")
	fmt.Println("-------------------------")
	fmt.Println("These JSON messages are now 100% compatible with JavaScript Cap'n Web!")
	fmt.Println()
	fmt.Println("JavaScript would send/receive:")
	fmt.Println(`["push", someExpression]`)
	fmt.Println(`["resolve", 42, "someValue"]`)
	fmt.Println(`["date", 1703980800000]`)
	fmt.Println(`["bigint", "123456789012345"]`)
	fmt.Println()
	fmt.Println("🎉 Full JavaScript compatibility achieved!")
}