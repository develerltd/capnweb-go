package capnweb

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// BidirectionalSession extends Session to support server-to-client calls
type BidirectionalSession struct {
	*Session

	// Client-side service registry
	clientServices map[string]interface{}
	servicesMux    sync.RWMutex

	// Callback management
	callbacks    map[CallbackID]*CallbackInfo
	callbacksMux sync.RWMutex
	nextCallbackID int64

	// Event subscriptions
	subscriptions    map[EventID]*Subscription
	subscriptionsMux sync.RWMutex

	// Configuration
	options BidirectionalOptions

	// Message processing
	processBidirectionalMessage func(Message)
}

// BidirectionalOptions configures bidirectional behavior
type BidirectionalOptions struct {
	// EnableServerCalls allows server to call client methods
	EnableServerCalls bool

	// EnableCallbacks allows callback function parameters
	EnableCallbacks bool

	// EnableEvents allows event subscription/emission
	EnableEvents bool

	// CallbackTimeout sets timeout for callback execution
	CallbackTimeout time.Duration

	// MaxConcurrentCallbacks limits concurrent callback execution
	MaxConcurrentCallbacks int

	// EventBufferSize sets buffer size for event channels
	EventBufferSize int
}

// DefaultBidirectionalOptions returns reasonable defaults
func DefaultBidirectionalOptions() BidirectionalOptions {
	return BidirectionalOptions{
		EnableServerCalls:      true,
		EnableCallbacks:        true,
		EnableEvents:           true,
		CallbackTimeout:        30 * time.Second,
		MaxConcurrentCallbacks: 10,
		EventBufferSize:        100,
	}
}

// CallbackID uniquely identifies a callback
type CallbackID string

// EventID uniquely identifies an event type
type EventID string

// CallbackInfo tracks callback function details
type CallbackInfo struct {
	ID        CallbackID
	Function  reflect.Value
	Type      reflect.Type
	ExportID  ExportID
	Created   time.Time
	LastUsed  time.Time
	UseCount  int64
}

// Subscription represents an event subscription
type Subscription struct {
	ID       SubscriptionID
	EventID  EventID
	Callback func(interface{})
	Channel  chan interface{}
	Active   bool
	Created  time.Time
}

// SubscriptionID uniquely identifies a subscription
type SubscriptionID string

// EventEmitter allows objects to emit events
type EventEmitter interface {
	EmitEvent(eventID EventID, data interface{}) error
	Subscribe(eventID EventID, callback func(interface{})) (SubscriptionID, error)
	Unsubscribe(subscriptionID SubscriptionID) error
}

// NewBidirectionalSession creates a new bidirectional RPC session
func NewBidirectionalSession(transport Transport, options ...BidirectionalOptions) (*BidirectionalSession, error) {
	opts := DefaultBidirectionalOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	// Create base session
	baseSession, err := NewSession(transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create base session: %w", err)
	}

	bs := &BidirectionalSession{
		Session:         baseSession,
		clientServices:  make(map[string]interface{}),
		callbacks:       make(map[CallbackID]*CallbackInfo),
		subscriptions:   make(map[EventID]*Subscription),
		options:         opts,
	}

	// Override message handlers to support bidirectional calls
	bs.setupBidirectionalHandlers()

	return bs, nil
}

// RegisterClientService registers a service that can be called by the server
func (bs *BidirectionalSession) RegisterClientService(name string, service interface{}) error {
	if !bs.options.EnableServerCalls {
		return fmt.Errorf("server calls are disabled")
	}

	// Validate service interface
	serviceType := reflect.TypeOf(service)
	if serviceType.Kind() != reflect.Ptr || serviceType.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("service must be a pointer to struct")
	}

	// Register all exported methods
	_ = reflect.ValueOf(service)
	numMethods := serviceType.NumMethod()

	for i := 0; i < numMethods; i++ {
		method := serviceType.Method(i)
		if !method.IsExported() {
			continue
		}

		// Validate method signature for RPC
		if err := bs.validateRPCMethod(method.Type); err != nil {
			return fmt.Errorf("method %s.%s invalid for RPC: %w", name, method.Name, err)
		}
	}

	bs.servicesMux.Lock()
	bs.clientServices[name] = service
	bs.servicesMux.Unlock()

	// Export the service so server can call it
	exportID, err := bs.exportValue(service)
	if err != nil {
		return fmt.Errorf("failed to export service: %w", err)
	}

	// Send service registration message to server
	regMsg := bs.protocol.NewPushMessage(map[string]interface{}{
		"type":      "registerService",
		"name":      name,
		"exportId":  exportID,
	}, nil)

	return bs.Send(context.Background(), regMsg)
}

// CreateCallback creates a callback function that can be passed as RPC parameter
func (bs *BidirectionalSession) CreateCallback(fn interface{}) (CallbackID, error) {
	if !bs.options.EnableCallbacks {
		return "", fmt.Errorf("callbacks are disabled")
	}

	fnValue := reflect.ValueOf(fn)
	fnType := fnValue.Type()

	if fnType.Kind() != reflect.Func {
		return "", fmt.Errorf("callback must be a function")
	}

	// Validate callback signature
	if err := bs.validateCallbackSignature(fnType); err != nil {
		return "", fmt.Errorf("invalid callback signature: %w", err)
	}

	// Generate callback ID
	bs.callbacksMux.Lock()
	bs.nextCallbackID++
	callbackID := CallbackID(fmt.Sprintf("callback_%d", bs.nextCallbackID))
	bs.callbacksMux.Unlock()

	// Export the callback
	exportID, err := bs.exportValue(fn)
	if err != nil {
		return "", fmt.Errorf("failed to export callback: %w", err)
	}

	// Store callback info
	callbackInfo := &CallbackInfo{
		ID:       callbackID,
		Function: fnValue,
		Type:     fnType,
		ExportID: exportID,
		Created:  time.Now(),
		LastUsed: time.Now(),
	}

	bs.callbacksMux.Lock()
	bs.callbacks[callbackID] = callbackInfo
	bs.callbacksMux.Unlock()

	return callbackID, nil
}

// CallCallback invokes a callback function
func (bs *BidirectionalSession) CallCallback(callbackID CallbackID, args ...interface{}) (interface{}, error) {
	bs.callbacksMux.RLock()
	callbackInfo, exists := bs.callbacks[callbackID]
	bs.callbacksMux.RUnlock()

	if !exists {
		return nil, fmt.Errorf("callback %s not found", callbackID)
	}

	// Update usage stats
	callbackInfo.LastUsed = time.Now()
	callbackInfo.UseCount++

	// Prepare arguments
	callArgs, err := bs.prepareCallbackArgs(callbackInfo.Type, args)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare callback arguments: %w", err)
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(context.Background(), bs.options.CallbackTimeout)
	defer cancel()

	// Execute callback in goroutine with timeout
	resultChan := make(chan interface{}, 1)
	errorChan := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				errorChan <- fmt.Errorf("callback panicked: %v", r)
			}
		}()

		results := callbackInfo.Function.Call(callArgs)

		// Handle return values
		if len(results) == 0 {
			resultChan <- nil
		} else if len(results) == 1 {
			resultChan <- results[0].Interface()
		} else {
			// Multiple return values - return as slice
			returnValues := make([]interface{}, len(results))
			for i, result := range results {
				returnValues[i] = result.Interface()
			}
			resultChan <- returnValues
		}
	}()

	// Wait for result or timeout
	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errorChan:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("callback timeout")
	}
}

// Subscribe subscribes to an event
func (bs *BidirectionalSession) Subscribe(eventID EventID, callback func(interface{})) (SubscriptionID, error) {
	if !bs.options.EnableEvents {
		return "", fmt.Errorf("events are disabled")
	}

	subscriptionID := SubscriptionID(fmt.Sprintf("sub_%d_%d", time.Now().UnixNano(), len(bs.subscriptions)))

	subscription := &Subscription{
		ID:       subscriptionID,
		EventID:  eventID,
		Callback: callback,
		Channel:  make(chan interface{}, bs.options.EventBufferSize),
		Active:   true,
		Created:  time.Now(),
	}

	bs.subscriptionsMux.Lock()
	bs.subscriptions[eventID] = subscription
	bs.subscriptionsMux.Unlock()

	// Start event processing goroutine
	go bs.processSubscription(subscription)

	// Send subscription message to server
	subMsg := bs.protocol.NewPushMessage(map[string]interface{}{
		"type":           "subscribe",
		"eventId":        string(eventID),
		"subscriptionId": string(subscriptionID),
	}, nil)

	err := bs.Send(context.Background(), subMsg)
	if err != nil {
		// Cleanup on failure
		bs.Unsubscribe(subscriptionID)
		return "", fmt.Errorf("failed to send subscription: %w", err)
	}

	return subscriptionID, nil
}

// Unsubscribe cancels an event subscription
func (bs *BidirectionalSession) Unsubscribe(subscriptionID SubscriptionID) error {
	bs.subscriptionsMux.Lock()
	defer bs.subscriptionsMux.Unlock()

	var eventID EventID
	var subscription *Subscription

	// Find subscription by ID
	for eid, sub := range bs.subscriptions {
		if sub.ID == subscriptionID {
			eventID = eid
			subscription = sub
			break
		}
	}

	if subscription == nil {
		return fmt.Errorf("subscription %s not found", subscriptionID)
	}

	// Mark as inactive and close channel
	subscription.Active = false
	close(subscription.Channel)

	// Remove from map
	delete(bs.subscriptions, eventID)

	// Send unsubscribe message to server
	unsubMsg := bs.protocol.NewPushMessage(map[string]interface{}{
		"type":           "unsubscribe",
		"subscriptionId": string(subscriptionID),
	}, nil)

	return bs.Send(context.Background(), unsubMsg)
}

// EmitEvent emits an event to subscribers
func (bs *BidirectionalSession) EmitEvent(eventID EventID, data interface{}) error {
	if !bs.options.EnableEvents {
		return fmt.Errorf("events are disabled")
	}

	// Send event message
	eventMsg := bs.protocol.NewPushMessage(map[string]interface{}{
		"type":    "event",
		"eventId": string(eventID),
		"data":    data,
	}, nil)

	return bs.Send(context.Background(), eventMsg)
}

// setupBidirectionalHandlers sets up message handlers for bidirectional communication
func (bs *BidirectionalSession) setupBidirectionalHandlers() {
	// Note: In a full implementation, this would override the session's message handlers
	// For now, we'll add a custom message processor
	bs.processBidirectionalMessage = func(msg Message) {
		if len(msg) >= 2 {
			// Check if this is a bidirectional message
			if expr, ok := msg[1].(map[string]interface{}); ok {
				if msgType, exists := expr["type"].(string); exists {
					switch msgType {
					case "clientCall":
						bs.handleClientCall(expr)
						return
					case "callbackInvoke":
						bs.handleCallbackInvoke(expr)
						return
					case "event":
						bs.handleEvent(expr)
						return
					}
				}
			}
		}
	}
}

// handleClientCall handles a call from server to client
func (bs *BidirectionalSession) handleClientCall(expr map[string]interface{}) {
	serviceName, _ := expr["service"].(string)
	methodName, _ := expr["method"].(string)
	args, _ := expr["args"].([]interface{})
	exportID, _ := expr["exportId"].(float64)

	bs.servicesMux.RLock()
	service, exists := bs.clientServices[serviceName]
	bs.servicesMux.RUnlock()

	if !exists {
		// Send error response
		bs.sendClientCallError(ExportID(exportID), fmt.Errorf("service %s not found", serviceName))
		return
	}

	// Execute method using reflection
	result, err := bs.executeClientMethod(service, methodName, args)
	if err != nil {
		bs.sendClientCallError(ExportID(exportID), err)
		return
	}

	// Send success response
	bs.sendClientCallResult(ExportID(exportID), result)
}

// handleCallbackInvoke handles callback invocation from server
func (bs *BidirectionalSession) handleCallbackInvoke(expr map[string]interface{}) {
	callbackIDStr, _ := expr["callbackId"].(string)
	args, _ := expr["args"].([]interface{})
	exportID, _ := expr["exportId"].(float64)

	result, err := bs.CallCallback(CallbackID(callbackIDStr), args...)
	if err != nil {
		bs.sendClientCallError(ExportID(exportID), err)
		return
	}

	bs.sendClientCallResult(ExportID(exportID), result)
}

// handleEvent handles incoming events
func (bs *BidirectionalSession) handleEvent(expr map[string]interface{}) {
	eventIDStr, _ := expr["eventId"].(string)
	data := expr["data"]

	eventID := EventID(eventIDStr)

	bs.subscriptionsMux.RLock()
	subscription, exists := bs.subscriptions[eventID]
	bs.subscriptionsMux.RUnlock()

	if exists && subscription.Active {
		// Send to subscription channel (non-blocking)
		select {
		case subscription.Channel <- data:
		default:
			// Channel full, drop event
		}
	}
}

// Helper methods

func (bs *BidirectionalSession) validateRPCMethod(methodType reflect.Type) error {
	// Method should have at least context parameter
	if methodType.NumIn() < 2 { // receiver + context
		return fmt.Errorf("method must have context.Context parameter")
	}

	// First parameter (after receiver) should be context
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if !methodType.In(1).Implements(contextType) {
		return fmt.Errorf("first parameter must be context.Context")
	}

	// Should return error as last parameter
	if methodType.NumOut() == 0 {
		return fmt.Errorf("method must return at least error")
	}

	errorType := reflect.TypeOf((*error)(nil)).Elem()
	lastOut := methodType.Out(methodType.NumOut() - 1)
	if !lastOut.Implements(errorType) {
		return fmt.Errorf("last return value must be error")
	}

	return nil
}

func (bs *BidirectionalSession) validateCallbackSignature(fnType reflect.Type) error {
	// Callbacks should be relatively simple functions
	if fnType.NumIn() > 5 {
		return fmt.Errorf("callback has too many parameters")
	}

	if fnType.NumOut() > 2 {
		return fmt.Errorf("callback has too many return values")
	}

	return nil
}

func (bs *BidirectionalSession) prepareCallbackArgs(fnType reflect.Type, args []interface{}) ([]reflect.Value, error) {
	numParams := fnType.NumIn()
	if len(args) != numParams {
		return nil, fmt.Errorf("expected %d arguments, got %d", numParams, len(args))
	}

	callArgs := make([]reflect.Value, numParams)
	for i, arg := range args {
		expectedType := fnType.In(i)
		argValue := reflect.ValueOf(arg)

		if !argValue.Type().AssignableTo(expectedType) {
			// Try to convert
			if argValue.Type().ConvertibleTo(expectedType) {
				argValue = argValue.Convert(expectedType)
			} else {
				return nil, fmt.Errorf("argument %d: cannot convert %s to %s", i, argValue.Type(), expectedType)
			}
		}

		callArgs[i] = argValue
	}

	return callArgs, nil
}

func (bs *BidirectionalSession) executeClientMethod(service interface{}, methodName string, args []interface{}) (interface{}, error) {
	serviceValue := reflect.ValueOf(service)
	method := serviceValue.MethodByName(methodName)

	if !method.IsValid() {
		return nil, fmt.Errorf("method %s not found", methodName)
	}

	// Prepare arguments (add context as first parameter)
	methodType := method.Type()
	callArgs := make([]reflect.Value, methodType.NumIn())
	callArgs[0] = reflect.ValueOf(context.Background())

	for i, arg := range args {
		if i+1 >= len(callArgs) {
			return nil, fmt.Errorf("too many arguments")
		}
		callArgs[i+1] = reflect.ValueOf(arg)
	}

	// Call method
	results := method.Call(callArgs)

	// Handle results
	if len(results) == 1 {
		// Only error returned
		if err, ok := results[0].Interface().(error); ok && err != nil {
			return nil, err
		}
		return nil, nil
	} else if len(results) == 2 {
		// Value and error returned
		if err, ok := results[1].Interface().(error); ok && err != nil {
			return nil, err
		}
		return results[0].Interface(), nil
	}

	return nil, fmt.Errorf("unexpected number of return values")
}

func (bs *BidirectionalSession) sendClientCallResult(exportID ExportID, result interface{}) {
	resolveMsg := bs.protocol.NewResolveMessage(exportID, result)
	bs.Send(context.Background(), resolveMsg)
}

func (bs *BidirectionalSession) sendClientCallError(exportID ExportID, err error) {
	rejectMsg := bs.protocol.NewRejectMessage(exportID, err.Error())
	bs.Send(context.Background(), rejectMsg)
}

func (bs *BidirectionalSession) processSubscription(subscription *Subscription) {
	for data := range subscription.Channel {
		if subscription.Active && subscription.Callback != nil {
			// Execute callback in separate goroutine to avoid blocking
			go func(d interface{}) {
				defer func() {
					if r := recover(); r != nil {
						// Log callback panic but don't crash
					}
				}()
				subscription.Callback(d)
			}(data)
		}
	}
}

// GetClientServices returns list of registered client services
func (bs *BidirectionalSession) GetClientServices() []string {
	bs.servicesMux.RLock()
	defer bs.servicesMux.RUnlock()

	services := make([]string, 0, len(bs.clientServices))
	for name := range bs.clientServices {
		services = append(services, name)
	}
	return services
}

// GetActiveCallbacks returns list of active callback IDs
func (bs *BidirectionalSession) GetActiveCallbacks() []CallbackID {
	bs.callbacksMux.RLock()
	defer bs.callbacksMux.RUnlock()

	callbacks := make([]CallbackID, 0, len(bs.callbacks))
	for id := range bs.callbacks {
		callbacks = append(callbacks, id)
	}
	return callbacks
}

// GetActiveSubscriptions returns list of active subscription IDs
func (bs *BidirectionalSession) GetActiveSubscriptions() []SubscriptionID {
	bs.subscriptionsMux.RLock()
	defer bs.subscriptionsMux.RUnlock()

	subscriptions := make([]SubscriptionID, 0, len(bs.subscriptions))
	for _, sub := range bs.subscriptions {
		if sub.Active {
			subscriptions = append(subscriptions, sub.ID)
		}
	}
	return subscriptions
}