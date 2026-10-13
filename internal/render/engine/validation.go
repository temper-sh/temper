package engine

import (
	"errors"
	"fmt"
	"regexp"
)

var stableOptionPattern = regexp.MustCompile(`^[a-z0-9]+(?:[_-][a-z0-9]+)*$`)

func validateChatRequest(request Request, engineName, artifactFormat string, allowedModalities ...[]string) error {
	if request.LayoutID == "" {
		return fmt.Errorf("%s layout id is required", engineName)
	}
	if request.ModelPath == "" {
		return fmt.Errorf("%s model path is required", engineName)
	}
	if request.ArtifactFormat != artifactFormat {
		return fmt.Errorf("%s artifact format %q must be %q", engineName, request.ArtifactFormat, artifactFormat)
	}
	if request.Interface != InterfaceChatCompletions {
		return fmt.Errorf("%s interface %q is unsupported", engineName, request.Interface)
	}
	if request.Window <= 0 {
		return fmt.Errorf("%s window must be greater than zero", engineName)
	}
	if request.MaxTokens <= 0 || request.MaxTokens >= request.Window {
		return fmt.Errorf("%s max tokens must be greater than zero and below window", engineName)
	}
	if request.Thinking != "on" && request.Thinking != "off" {
		return fmt.Errorf("%s thinking %q must be on or off", engineName, request.Thinking)
	}
	if request.ChatTemplatePath != "" {
		return fmt.Errorf("%s does not support Temper chat-template patches", engineName)
	}
	if request.NGL != nil {
		return fmt.Errorf("%s does not support llama.cpp layer offload placement", engineName)
	}
	for _, allowed := range allowedModalities {
		if equalStrings(request.Modalities, allowed) {
			return nil
		}
	}
	return fmt.Errorf("%s modalities %v are unsupported", engineName, request.Modalities)
}

func validatePositiveBatching(engineName string, values ...int) error {
	for _, value := range values {
		if value <= 0 {
			return fmt.Errorf("%s batching values must be greater than zero", engineName)
		}
	}
	return nil
}

func validateFraction(engineName, field string, value float64) error {
	if value <= 0 || value > 1 {
		return fmt.Errorf("%s %s must be greater than zero and at most one", engineName, field)
	}
	return nil
}

func validateOnOff(engineName, field, value string) error {
	if value != "on" && value != "off" {
		return fmt.Errorf("%s %s %q must be on or off", engineName, field, value)
	}
	return nil
}

func validateSpeculation(engineName, method string, tokens int) error {
	switch method {
	case "none":
		if tokens != 0 {
			return fmt.Errorf("%s speculative tokens require mtp speculation", engineName)
		}
	case "mtp":
		if tokens <= 0 || tokens > 16 {
			return fmt.Errorf("%s MTP speculative tokens must be between 1 and 16", engineName)
		}
	default:
		return fmt.Errorf("%s speculation %q must be none or mtp", engineName, method)
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func requireStableOption(engineName, field, value string) error {
	if !stableOptionPattern.MatchString(value) {
		return errors.New(engineName + " " + field + " must be a lowercase stable option id")
	}
	return nil
}
