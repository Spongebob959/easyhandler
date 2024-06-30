package easyhandler

import (
	"fmt"
	"reflect"
)

type Result[T any] struct {
	Values []T
	Err   error
}

func Ok[T any](values ...T) Result[T] {
	return Result[T]{Values: values}
}

func Err[T any](err error) Result[T] {
	return Result[T]{Err: err}
}
func (r *Result[T]) IsOk() (bool) {
	return r.Err == nil
}

func (r *Result[T]) IsErr() (bool) {
	return r.Err != nil
}

func handleArgs(args ...interface{}) ([]reflect.Value) {
	inputs := make([]reflect.Value, len(args))
	for i, arg := range args {
		inputs[i] = reflect.ValueOf(arg)
	}
	return inputs
}

func Wrap(function interface{}, args ...interface{}) func() Result[any] {
	return func() Result[any] {
		var funcValue = reflect.ValueOf(function)
		var funcType = funcValue.Type()
		if funcType.Kind() != reflect.Func {
			return Err[any](fmt.Errorf("no function provided"))
		}
		if len(args) != funcType.NumIn() {
			return Err[any](fmt.Errorf("argument count does not match function's parameter count"))
		}
		inputs := handleArgs(args...)
		var results = funcValue.Call(inputs)
		if funcType.NumOut() == 0 {
			return Ok[any]()
		}
		var lastIndex = len(results) - 1
		if funcType.Out(lastIndex).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			errValue := results[lastIndex].Interface()
			if errValue != nil {
				return Err[any](errValue.(error))
			}
			results = results[:lastIndex]
		}
		var values = make([]any, len(results))
		for i, res := range results {
			values[i] = res.Interface()
		}	
		return Ok(values...)
	}
}

func Try(handler func(error), fns ...func() Result[any]) ([]any, Result[any]) {
	var results = []any{}
	if len(fns) == 0 {
		return nil, Err[any](fmt.Errorf("no functions provided"))
	}
	for i := 0; i < len(fns); i++ {
		var res = fns[i]()
		if res.IsErr() {
			handler(res.Err)
		} else {
			results = append(results, res.Values...)
		}
	}
	var null any
	return results, Ok(null)
}