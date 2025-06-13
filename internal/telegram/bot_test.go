package telegram

import (
	"os"
	"testing"

	"lime-bot/internal/config"
	"lime-bot/internal/db"
)

func TestMain(m *testing.M) {
	
	os.Exit(m.Run())
}

func setupTestService(t *testing.T) (*Service, *db.Repository) {
	
	cfg := &config.Config{
		BotToken:     "test_token",
		WGAgentAddr:  "localhost:8080",
		SuperAdminID: "123456789",
	}

	
	repo, err := db.NewRepository(":memory:")
	if err != nil {
		t.Fatalf("failed to create test repository: %v", err)
	}

	
	if err := repo.AutoMigrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	
	setupTestData(t, repo)

	
	service := &Service{
		repo: repo,
		cfg:  cfg,
	}

	return service, repo
}

func setupTestData(t *testing.T, repo *db.Repository) {
	
	plans := []db.Plan{
		{Name: "Тест 1 месяц", PriceInt: 200, DurationDays: 30},
		{Name: "Тест 3 месяца", PriceInt: 500, DurationDays: 90},
		{Name: "Архивный тариф", PriceInt: 100, DurationDays: 7, Archived: true},
	}

	for _, plan := range plans {
		repo.DB().Create(&plan)
	}

	
	methods := []db.PaymentMethod{
		{Bank: "Сбербанк", PhoneNumber: "+79001234567"},
		{Bank: "Тинькофф", PhoneNumber: "+79007654321"},
		{Bank: "Архивный банк", PhoneNumber: "+79009999999", Archived: true},
	}

	for _, method := range methods {
		repo.DB().Create(&method)
	}

	
	user := db.User{
		TgID:     123456789,
		Username: "testuser",
		RefCode:  "testref123",
	}
	repo.DB().Create(&user)

	
	admin := db.Admin{
		TgID: 123456789,
		Role: "super",
	}
	repo.DB().Create(&admin)
}

func TestIsAdmin(t *testing.T) {
	service, _ := setupTestService(t)

	tests := []struct {
		name     string
		userID   int64
		expected bool
	}{
		{
			name:     "Super admin from config",
			userID:   123456789,
			expected: true,
		},
		{
			name:     "Non-admin user",
			userID:   987654321,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.isAdmin(tt.userID)
			if result != tt.expected {
				t.Errorf("isAdmin(%d) = %v, want %v", tt.userID, result, tt.expected)
			}
		})
	}
}

func TestGenerateRefCode(t *testing.T) {
	_, _ = setupTestService(t)

	userID := int64(123456789)
	code := generateRefCode(userID)

	if code == "" {
		t.Error("generateRefCode returned empty string")
	}

	if len(code) > 12 {
		t.Errorf("generateRefCode returned code longer than 12 chars: %s", code)
	}

	
	if len(code) < 8 {
		t.Errorf("generateRefCode returned suspiciously short code: %s", code)
	}
}


func TestStartsWith(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		prefix   string
		expected bool
	}{
		{
			name:     "Valid prefix",
			s:        "ref_abc123",
			prefix:   "ref_",
			expected: true,
		},
		{
			name:     "Invalid prefix",
			s:        "abc123",
			prefix:   "ref_",
			expected: false,
		},
		{
			name:     "Empty string",
			s:        "",
			prefix:   "ref_",
			expected: false,
		},
		{
			name:     "Exact match",
			s:        "ref_",
			prefix:   "ref_",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := startsWith(tt.s, tt.prefix)
			if result != tt.expected {
				t.Errorf("startsWith(%q, %q) = %v, want %v", tt.s, tt.prefix, result, tt.expected)
			}
		})
	}
}

func TestBotError(t *testing.T) {
	err := NewBotError("TEST_CODE", "Test message", "User message", "Details")

	if err.Code != "TEST_CODE" {
		t.Errorf("Expected code TEST_CODE, got %s", err.Code)
	}

	if err.UserMessage != "User message" {
		t.Errorf("Expected user message 'User message', got %s", err.UserMessage)
	}

	errorString := err.Error()
	if errorString == "" {
		t.Error("Error() returned empty string")
	}

	
	if len(errorString) < 10 {
		t.Errorf("Error() returned suspiciously short string: %s", errorString)
	}
}


func TestErrorHelpers(t *testing.T) {
	tests := []struct {
		name     string
		errFunc  func() *BotError
		wantCode string
	}{
		{
			name: "ErrInvalidInputf",
			errFunc: func() *BotError {
				return ErrInvalidInputf("test details %s", "arg")
			},
			wantCode: ErrInvalidInput,
		},
		{
			name: "ErrDatabasef",
			errFunc: func() *BotError {
				return ErrDatabasef("db error")
			},
			wantCode: ErrDatabaseError,
		},
		{
			name: "ErrPermission",
			errFunc: func() *BotError {
				return ErrPermission("no permission")
			},
			wantCode: ErrPermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.errFunc()
			if err.Code != tt.wantCode {
				t.Errorf("Expected code %s, got %s", tt.wantCode, err.Code)
			}
			if err.UserMessage == "" {
				t.Error("UserMessage should not be empty")
			}
		})
	}
}
