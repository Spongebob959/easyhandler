package handler
import (
	"context"
	"fmt"
	"log"
	"reflect"
	"runtime"
	"sync"
	"time"
)

// Result struct to hold values or an error
type Result[T any] struct {
	Values []T
	Err    error
}

// Ok function to create a Result with values
func Ok[T any](values ...T) Result[T] {
	return Result[T]{Values: values}
}

// Err function to create a Result with an error
func Err[T any](err error) Result[T] {
	return Result[T]{Err: err}
}

// Methods to check if the Result contains an error or values
func (r *Result[T]) IsOk() bool {
	return r.Err == nil
}

func (r *Result[T]) IsErr() bool {
	return r.Err != nil
}

// FunctionHandler interface definition
type FunctionHandler interface {
	ConvertArgs(args ...interface{}) []reflect.Value
	WrapFunction(function interface{}, args ...interface{}) func() Result[any]
	WrapErrorHandler(handlerFunc interface{}) Result[HandlerValues]
	Try(handler interface{}, funcs ...func() Result[any]) ([]any, Result[any])
	SetTimeout(duration time.Duration)
	SetRetry(retries int)
	SetParallel(isParallel bool)
}

// FunctionHandlerImpl struct to implement FunctionHandler interface
type FunctionHandlerImpl struct {
	timeout   time.Duration
	retries   int
	isParallel bool
}

// HandlerValues struct to hold function values
type HandlerValues struct {
	Args []reflect.Value
	Func *reflect.Value
}

// SetTimeout method to set timeout duration
func (fhi *FunctionHandlerImpl) SetTimeout(duration time.Duration) {
	fhi.timeout = duration
}

// SetRetry method to set retry attempts
func (fhi *FunctionHandlerImpl) SetRetry(retries int) {
	fhi.retries = retries
}

// SetParallel method to enable or disable parallel execution
func (fhi *FunctionHandlerImpl) SetParallel(isParallel bool) {
	fhi.isParallel = isParallel
}

// ConvertArgs method to convert arguments to reflect values
func (fhi *FunctionHandlerImpl) ConvertArgs(args ...interface{}) []reflect.Value {
	inputs := make([]reflect.Value, len(args))
	for i, arg := range args {
		inputs[i] = reflect.ValueOf(arg)
	}
	return inputs
}

// WrapFunction method to create a function that returns a Result
func (fhi *FunctionHandlerImpl) WrapFunction(function interface{}, args ...interface{}) func() Result[any] {
	return func() Result[any] {
		funcValue := reflect.ValueOf(function)
		funcType := funcValue.Type()
		if funcType.Kind() != reflect.Func {
			err := fmt.Errorf("no function provided")
			fhi.LogError(err)
			return Err[any](err)
		}
		if len(args) != funcType.NumIn() {
			err := fmt.Errorf("argument count does not match function's parameter count")
			fhi.LogError(err)
			return Err[any](err)
		}
		inputs := fhi.ConvertArgs(args...)
		results := funcValue.Call(inputs)
		if funcType.NumOut() == 0 {
			return Ok[any]()
		}
		lastIndex := len(results) - 1
		if funcType.Out(lastIndex).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			errValue := results[lastIndex].Interface()
			if errValue != nil {
				err := errValue.(error)
				fhi.LogError(err)
				return Err[any](err)
			}
			results = results[:lastIndex]
		}
		values := make([]any, len(results))
		for i, res := range results {
			values[i] = res.Interface()
		}
		return Ok(values...)
	}
}

// WrapErrorHandler method to wrap an error handler function
func (fhi *FunctionHandlerImpl) WrapErrorHandler(handlerFunc interface{}) Result[HandlerValues] {
	handlerValue := reflect.ValueOf(handlerFunc)
	handlerType := handlerValue.Type()
	if handlerType.Kind() != reflect.Func {
		err := fmt.Errorf("provided handler is not a function")
		fhi.LogError(err)
		return Err[HandlerValues](err)
	}
	if handlerType.NumIn() != 1 || handlerType.In(0) != reflect.TypeOf((*error)(nil)).Elem() {
		err := fmt.Errorf("the error handler must take an error as an arg")
		fhi.LogError(err)
		return Err[HandlerValues](err)
	}
	if handlerType.NumOut() > 1 || (handlerType.NumOut() == 1 && !handlerType.Out(0).Implements(reflect.TypeOf((*error)(nil)).Elem())) {
		err := fmt.Errorf("the error handler must return at most one error")
		fhi.LogError(err)
		return Err[HandlerValues](err)
	}
	return Ok(HandlerValues{Func: &handlerValue})
}

// Try method to handle multiple functions and an error handler with optional parallelism
func (fhi *FunctionHandlerImpl) Try(handler interface{}, funcs ...func() Result[any]) ([]any, Result[any]) {
	results := []any{}
	handlerFunc := fhi.WrapErrorHandler(handler)
	if handlerFunc.IsErr() {
		err := fmt.Errorf("invalid error handler")
		fhi.LogError(err)
		return nil, Err[any](err)
	}
	if len(funcs) == 0 {
		err := fmt.Errorf("no functions provided")
		fhi.LogError(err)
		return nil, Err[any](err)
	}
	if fhi.isParallel {
		var wg sync.WaitGroup
		resultCh := make(chan Result[any], len(funcs))
		for _, fn := range funcs {
			wg.Add(1)
			go func(fn func() Result[any]) {
				defer wg.Done()
				var res Result[any]
				if fhi.timeout > 0 {
					ctx, cancel := context.WithTimeout(context.Background(), fhi.timeout)
					defer cancel()
					ch := make(chan Result[any], 1)
					go func() {
						ch <- fhi.retryFunction(fn)
					}()
					select {
					case res = <-ch:
					case <-ctx.Done():
						err := fmt.Errorf("function timed out")
						fhi.LogError(err)
						res = Err[any](err)
					}
				} else {
					res = fhi.retryFunction(fn)
				}
				resultCh <- res
			}(fn)
		}
		wg.Wait()
		close(resultCh)
		for res := range resultCh {
			if res.IsErr() {
				handlerResults := handlerFunc.Values[0].Func.Call([]reflect.Value{reflect.ValueOf(res.Err)})
				if len(handlerResults) == 1 {
					if handlerError, ok := handlerResults[0].Interface().(error); ok && handlerError != nil {
						fhi.LogError(handlerError)
						return nil, Err[any](handlerError)
					}
				}
			} else {
				results = append(results, res.Values...)
			}
		}
	} else {
		for _, fn := range funcs {
			var res Result[any]
			if fhi.timeout > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), fhi.timeout)
				defer cancel()
				ch := make(chan Result[any], 1)
				go func() {
					ch <- fhi.retryFunction(fn)
				}()
				select {
				case res = <-ch:
				case <-ctx.Done():
					err := fmt.Errorf("function timed out")
					fhi.LogError(err)
					res = Err[any](err)
				}
			} else {
				res = fhi.retryFunction(fn)
			}
			if res.IsErr() {
				handlerResults := handlerFunc.Values[0].Func.Call([]reflect.Value{reflect.ValueOf(res.Err)})
				if len(handlerResults) == 1 {
					if handlerError, ok := handlerResults[0].Interface().(error); ok && handlerError != nil {
						fhi.LogError(handlerError)
						return nil, Err[any](handlerError)
					}
				}
			} else {
				results = append(results, res.Values...)
			}
		}
	}
	return results, Ok[any](nil)
}

// retryFunction method to handle retry logic
func (fhi *FunctionHandlerImpl) retryFunction(fn func() Result[any]) Result[any] {
	var res Result[any]
	for i := 0; i <= fhi.retries; i++ {
		res = fn()
		if res.IsOk() {
			return res
		}
		fhi.LogError(res.Err)
		time.Sleep(time.Second) // Backoff can be added here
	}
	return res
}

// LogError logs the error with file and line number information, very useful for the errorhandler
func (fhi *FunctionHandlerImpl) LogError(err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(2) // Adjusted to capture the correct call stack frame
		log.Printf("[ERROR] %s:%d %v", file, line, err)
	}
}

