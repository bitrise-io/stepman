package validator

import "fmt"

type MissingPropertyError struct {
	Property string
}

func NewMissingPropertyError(property string) MissingPropertyError {
	return MissingPropertyError{Property: property}
}

func (e MissingPropertyError) Error() string {
	return fmt.Sprintf("missing property: %s", e.Property)
}

type InvalidPropertyError struct {
	Property string
	Reason   string
}

func NewInvalidPropertyError(property, reason string) InvalidPropertyError {
	return InvalidPropertyError{Property: property, Reason: reason}
}

func (e InvalidPropertyError) Error() string {
	return fmt.Sprintf("invalid property: %s: %s", e.Property, e.Reason)
}

type MissingEnvVarPropertyError struct {
	Key      string
	Property string
}

func NewMissingEnvVarPropertyError(key, property string) MissingEnvVarPropertyError {
	return MissingEnvVarPropertyError{Key: key, Property: property}
}

func (e MissingEnvVarPropertyError) Error() string {
	return fmt.Sprintf("%s: missing property: %s", e.Key, e.Property)
}

type InvalidEnvVarPropertyError struct {
	Key      string
	Property string
	Reason   string
}

func NewInvalidEnvVarPropertyError(key, property, reason string) InvalidEnvVarPropertyError {
	return InvalidEnvVarPropertyError{Key: key, Property: property, Reason: reason}
}

func (e InvalidEnvVarPropertyError) Error() string {
	return fmt.Sprintf("%s: invalid property: %s: %s", e.Key, e.Property, e.Reason)
}
