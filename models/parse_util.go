package models

import (
	"fmt"
)

func normalizeMapStringInterface(source map[string]interface{}) (map[string]interface{}, error) {
	target, err := castRecursiveToMapStringInterface(source)
	if err != nil {
		return nil, err
	}
	castedTarget, ok := target.(map[string]interface{})
	if !ok {
		return nil, err
	}
	return castedTarget, nil
}

func castRecursiveToMapStringInterface(source interface{}) (interface{}, error) {
	if array, ok := source.([]interface{}); ok {
		var convertedArray []interface{}
		for _, element := range array {
			convertedValue, err := castRecursiveToMapStringInterface(element)
			if err != nil {
				return nil, err
			}
			convertedArray = append(convertedArray, convertedValue)
		}
		return convertedArray, nil
	}

	if interfaceToInterfaceMap, ok := source.(map[interface{}]interface{}); ok {
		target := make(map[string]interface{})
		for key, value := range interfaceToInterfaceMap {
			strKey, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("failed to convert map key from type interface{} to string")
			}

			convertedValue, err := castRecursiveToMapStringInterface(value)
			if err != nil {
				return nil, err
			}
			target[strKey] = convertedValue
		}
		return target, nil
	}

	if stringToInterfaceMap, ok := source.(map[string]interface{}); ok {
		target := make(map[string]interface{})
		for key, value := range stringToInterfaceMap {
			convertedValue, err := castRecursiveToMapStringInterface(value)
			if err != nil {
				return nil, err
			}
			target[key] = convertedValue
		}
		return target, nil
	}

	return source, nil
}
