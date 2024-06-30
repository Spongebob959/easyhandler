package handler

import (
	"fmt"
	"reflect"
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
}

// FunctionHandlerImpl struct to implement FunctionHandler interface
type FunctionHandlerImpl struct{}

// HandlerValues struct to hold function values
type HandlerValues struct {
	Args []reflect.Value
	Func *reflect.Value
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
			return Err[any](fmt.Errorf("no function provided"))
		}
		if len(args) != funcType.NumIn() {
			return Err[any](fmt.Errorf("argument count does not match function's parameter count"))
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
				return Err[any](errValue.(error))
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
		return Err[HandlerValues](fmt.Errorf("provided handler is not a function"))
	}
	if handlerType.NumIn() != 1 || handlerType.In(0) != reflect.TypeOf((*error)(nil)).Elem() {
		return Err[HandlerValues](fmt.Errorf("the error handler must take an error as an arg"))
	}
	if handlerType.NumOut() > 1 || (handlerType.NumOut() == 1 && !handlerType.Out(0).Implements(reflect.TypeOf((*error)(nil)).Elem())) {
		return Err[HandlerValues](fmt.Errorf("the error handler must return at most one error"))
	}
	return Ok(HandlerValues{Func: &handlerValue})
}

// Try method to handle multiple functions and an error handler
func (fhi *FunctionHandlerImpl) Try(handler interface{}, funcs ...func() Result[any]) ([]any, Result[any]) {
	results := []any{}
	handlerFunc := fhi.WrapErrorHandler(handler)
	if handlerFunc.IsErr() {
		return nil, Err[any](fmt.Errorf("invalid error handler"))
	}
	if len(funcs) == 0 {
		return nil, Err[any](fmt.Errorf("no functions provided"))
	}
	for _, fn := range funcs {
		res := fn()
		if res.IsErr() {
			handlerResults := handlerFunc.Values[0].Func.Call([]reflect.Value{reflect.ValueOf(res.Err)})
			if len(handlerResults) == 1 {
				if handlerError, ok := handlerResults[0].Interface().(error); ok && handlerError != nil {
					return nil, Err[any](handlerError)
				}
			}
		} else {
			results = append(results, res.Values...)
		}
	}
	return results, Ok[any](nil)
}

