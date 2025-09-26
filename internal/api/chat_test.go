package api

import "testing"

func float64Ptr(v float64) *float64 { return &v }
func intPtr(v int) *int             { return &v }

func validRequest() ChatCompletionRequest {
	return ChatCompletionRequest{
		Model:    "mock-model",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}
}

func TestChatCompletionRequest_Validate_Valid(t *testing.T) {
	req := validRequest()
	req.Temperature = float64Ptr(0.7)
	req.TopP = float64Ptr(0.9)
	req.MaxTokens = intPtr(128)

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestChatCompletionRequest_Validate_EmptyModel(t *testing.T) {
	req := validRequest()
	req.Model = "  "

	err := req.Validate()
	assertFieldError(t, err, "model")
}

func TestChatCompletionRequest_Validate_NoMessages(t *testing.T) {
	req := validRequest()
	req.Messages = nil

	err := req.Validate()
	assertFieldError(t, err, "messages")
}

func TestChatCompletionRequest_Validate_InvalidRole(t *testing.T) {
	req := validRequest()
	req.Messages = []ChatMessage{{Role: "wizard", Content: "hi"}}

	err := req.Validate()
	assertFieldError(t, err, "messages[0].role")
}

func TestChatCompletionRequest_Validate_EmptyMessageContent(t *testing.T) {
	req := validRequest()
	req.Messages = []ChatMessage{{Role: "user", Content: "   "}}

	err := req.Validate()
	assertFieldError(t, err, "messages[0].content")
}

func TestChatCompletionRequest_Validate_TemperatureOutOfRange(t *testing.T) {
	for _, temp := range []float64{-0.1, 2.1} {
		req := validRequest()
		req.Temperature = float64Ptr(temp)

		err := req.Validate()
		assertFieldError(t, err, "temperature")
	}
}

func TestChatCompletionRequest_Validate_TopPOutOfRange(t *testing.T) {
	for _, topP := range []float64{0, -0.1, 1.1} {
		req := validRequest()
		req.TopP = float64Ptr(topP)

		err := req.Validate()
		assertFieldError(t, err, "top_p")
	}
}

func TestChatCompletionRequest_Validate_MaxTokensNotPositive(t *testing.T) {
	for _, mt := range []int{0, -1} {
		req := validRequest()
		req.MaxTokens = intPtr(mt)

		err := req.Validate()
		assertFieldError(t, err, "max_tokens")
	}
}

func TestChatCompletionRequest_Validate_AggregatesAllErrors(t *testing.T) {
	req := ChatCompletionRequest{
		Model:       "",
		Messages:    nil,
		Temperature: float64Ptr(9),
	}

	err := req.Validate()
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("error type = %T, want *ValidationError", err)
	}
	if len(ve.Errors) != 3 {
		t.Errorf("len(Errors) = %d, want 3, got %v", len(ve.Errors), ve.Errors)
	}
}

func assertFieldError(t *testing.T, err error, field string) {
	t.Helper()

	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("error type = %T, want *ValidationError", err)
	}
	for _, fe := range ve.Errors {
		if fe.Field == field {
			return
		}
	}
	t.Errorf("no error for field %q, got %+v", field, ve.Errors)
}
