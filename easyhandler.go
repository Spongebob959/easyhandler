package main

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

type EasyHandler interface {
	HandleArgs(args ...interface{}) ([]reflect.Value)
	Wrap(function interface{}, args ...interface{}) (func() Result[any])
	wrapErrHandler(handlerfunc interface{}, args ...interface{}) (Result[FuncValues])
	Try(handler interface{}, fns ...func() Result[any]) ([]any, Result[any])
}

type EasyhandlerImpl struct {}

type FuncValues struct {
	Args []reflect.Value
	Func *reflect.Value
}

func (esi *EasyhandlerImpl) HandleArgs(args ...interface{}) ([]reflect.Value) {
	inputs := make([]reflect.Value, len(args))
	for i, arg := range args {
		inputs[i] = reflect.ValueOf(arg)
	}
	return inputs
}

func (esi *EasyhandlerImpl) Wrap(function interface{}, args ...interface{}) (func() Result[any]) {
	return func() Result[any] {
		var funcValue = reflect.ValueOf(function)
		var funcType = funcValue.Type()
		if funcType.Kind() != reflect.Func {
			return Err[any](fmt.Errorf("no function provided"))
		}
		if len(args) != funcType.NumIn() {
			return Err[any](fmt.Errorf("argument count does not match function's parameter count"))
		}
		inputs := esi.HandleArgs(args...)
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


func (esi *EasyhandlerImpl) wrapErrHandler(handlerfunc interface{}, args ...interface{}) (Result[FuncValues]) {
	var handlerValue = reflect.ValueOf(handlerfunc) 
	var handlerType = handlerValue.Type()
	var in = esi.HandleArgs(args...)
	if handlerType.Kind() != reflect.Func {
		return Err[FuncValues](fmt.Errorf("provided handler is not a function"))
	}
	if handlerType.NumIn() != 1 || handlerType.In(0) != reflect.TypeOf((*error)(nil)).Elem() {
		return Err[FuncValues](fmt.Errorf("the error handler must take an error as an arg"))
	}
	return Result[FuncValues]{ Values: []FuncValues{ { Args: in, Func: &handlerValue, }, } ,}
}

func (esi *EasyhandlerImpl) Try(handler interface{}, fns ...func() Result[any]) ([]any, Result[any]) {
	var results = []any{}
	var handlerfunc = esi.wrapErrHandler(handler)
	if len(fns) == 0 {
		return nil, Err[any](fmt.Errorf("no functions provided"))
	}
	for i := 0; i < len(fns); i++ {
		var res = fns[i]()
		if res.IsErr() {
			_ = handlerfunc.Values[0].Func.Call(handlerfunc.Values[0].Args)
		} else {
			results = append(results, res.Values...)
		}
	}
	var null any
	return results, Ok(null)
}