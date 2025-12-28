package services

import (
	"array-assessment/internal/config"
	"array-assessment/internal/dto"
	"array-assessment/internal/models"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

type AuthTransport struct {
	apiKey string
	base   http.RoundTripper
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return t.base.RoundTrip(req)
}

// NorthWindService handles customer search operations
type NorthWindService struct {
	config *config.NorthWindConfig
	client *http.Client
	logger *slog.Logger
}

// NewNorthWindService creates a new NorthWind service
func NewNorthWindService(
	cfg *config.NorthWindConfig,
	logger *slog.Logger,
) NorthWindServiceInterface {

	transport := &AuthTransport{
		apiKey: cfg.ApiKey,
		base:   http.DefaultTransport,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	return &NorthWindService{
		config: cfg,
		client: client,
		logger: logger,
	}
}

func (s *NorthWindService) buildRequest(
	ctx context.Context,
	method, path string,
	body any,
) (*http.Request, error) {

	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		buf = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		method,
		path,
		buf,
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	return req, nil
}

func (s *NorthWindService) do(req *http.Request) (*http.Response, []byte, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error(
			"northwind request failed",
			"method", req.Method,
			"url", req.URL.String(),
			"error", err,
		)
		return nil, nil, err
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		return nil, nil, fmt.Errorf("read response body: %w", err)
	}

	return resp, body, nil
}

func (s *NorthWindService) AuthAccount(ctx context.Context, requestDto dto.NorthWindAccountRequestDto) (*dto.NorthWindAccountValidationResult, error) {
	req, err := s.buildRequest(
		ctx,
		http.MethodPost,
		s.config.BaseUrl+"/external/accounts/validate",
		requestDto,
	)

	s.logger.Info("validating account", "accountHolderName", requestDto.AccountHolderName)

	if err != nil {
		return nil, err
	}

	resp, body, err := s.do(req)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {

	case http.StatusOK:
		var success dto.NorthWindValidateResponse[dto.NorthWindAccountData]
		if err := json.Unmarshal(body, &success); err != nil {
			return nil, fmt.Errorf("decode success response: %w", err)
		}

		accountExists := success.Data != nil && success.Data.AccountID != ""
		accountValid := success.Validation.Valid

		s.logger.Info(
			"northwind validation result",
			"account_exists", accountExists,
			"account_valid", accountValid,
			"account_id", success.Data.AccountID,
		)

		return &dto.NorthWindAccountValidationResult{
			Response:         &success,
			AvailableBalance: success.Data.AvailableBalance,
			AccountExists:    accountExists,
			AccountValid:     accountValid,
		}, nil

	case http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusInternalServerError:

		var errResp dto.NorthwindValidateAccountErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			return nil, fmt.Errorf(
				"northwind error (%d): %s",
				resp.StatusCode,
				string(body),
			)
		}

		s.logger.Error(
			"northwind validation error",
			"status", resp.StatusCode,
			"code", errResp.Error.Code,
			"message", errResp.Error.Message,
			"request_id", errResp.Error.RequestID,
		)

		return nil, errors.New(errResp.Error.Message)

	default:
		return nil, fmt.Errorf(
			"unexpected northwind response (%d): %s",
			resp.StatusCode,
			string(body),
		)
	}
}

func (s *NorthWindService) ValidateTransfer(
	referenceNumber string,
	sourceUser, destinationUser *models.User,
	fromAccount, toAccount *models.Account,
	amount decimal.Decimal,
	currency, transferType,
	description, idempotencyKey, direction string,
) (*dto.NorthWindValidateResponse[dto.NorthWindTransferStatusResponse], error) {
	ctx := context.Background()

	sourceAccount := dto.NorthWindTransferAccount{
		AccountHolderName: sourceUser.FullName(),
		AccountNumber:     fromAccount.AccountNumber,
		RoutingNumber:     fromAccount.RoutingNumber,
		InstitutionName:   "Acme Bank",
	}

	destinationAccount := dto.NorthWindTransferAccount{
		AccountHolderName: destinationUser.FullName(),
		AccountNumber:     toAccount.AccountNumber,
		RoutingNumber:     toAccount.RoutingNumber,
		InstitutionName:   "Acme Bank",
	}

	// ---- Transfer Validation Request ----

	validateReq := dto.NorthWindTransferValidationRequest{
		Amount:             amount.InexactFloat64(),
		Currency:           "USD",
		SourceAccount:      sourceAccount,
		DestinationAccount: destinationAccount,
		Direction:          "INBOUND",
		TransferType:       "ACH",
		Description:        description,
		ReferenceNumber:    referenceNumber,
		ScheduledDate:      time.Now().UTC().Format(time.RFC3339),
	}

	req, err := s.buildRequest(
		ctx,
		http.MethodPost,
		"/external/transfers/validate",
		validateReq,
	)

	if err != nil {
		return nil, err
	}

	resp, body, err := s.do(req)

	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {

	case http.StatusOK:
		var success dto.NorthWindValidateResponse[dto.NorthWindTransferStatusResponse]
		if err := json.Unmarshal(body, &success); err != nil {
			return nil, fmt.Errorf("decode success response: %w", err)
		}

		s.logger.Info(
			"northwind transfer validation result",
			"success", success.Validation.Valid,
		)

		return &success, nil

	case http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusInternalServerError:

		var errResp dto.NorthwindValidateAccountErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			return nil, fmt.Errorf(
				"northwind error (%d): %s",
				resp.StatusCode,
				string(body),
			)
		}

		s.logger.Info(
			"northwind transfer validation result",
			"failed", errResp.Error.Message,
		)

		return nil, fmt.Errorf(
			"northwind error (%d): %s",
			resp.StatusCode,
			errResp.Error.Message,
		)

	default:
		return nil, fmt.Errorf(
			"unexpected northwind status code (%d): %s",
			resp.StatusCode,
			string(body),
		)
	}
}

func (s *NorthWindService) InitiateTransfer(
	ctx context.Context,
	transferDto dto.NorthWindInitiateTransferRequest,
) (*dto.NorthWindTransferStatusResponse, error) {

	req, err := s.buildRequest(
		ctx,
		http.MethodPost,
		s.config.BaseUrl+"/external/transfers/initiate",
		transferDto,
	)

	if err != nil {
		return nil, err
	}

	resp, body, err := s.do(req)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {

	case http.StatusCreated:
		var success dto.NorthWindTransferStatusResponse
		if err := json.Unmarshal(body, &success); err != nil {
			return nil, fmt.Errorf("decode success response: %w", err)
		}

		s.logger.Info("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		s.logger.Info("respond data", "success", success)
		s.logger.Info("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")

		s.logger.Info(
			"northwind transfer initiate result",
			"success", success,
		)

		return &success, nil

	case http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusInternalServerError:

		var errResp dto.NorthwindValidateAccountErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			return nil, fmt.Errorf(
				"northwind error (%d): %s",
				resp.StatusCode,
				string(body),
			)
		}

		s.logger.Error("Northwind Initiate Error", "failed", errResp.Error.Message)

		return nil, fmt.Errorf(
			"northwind error (%d): %s",
			resp.StatusCode,
			errResp.Error.Message,
		)

	default:
		return nil, fmt.Errorf(
			"unexpected northwind status code (%d): %s",
			resp.StatusCode,
			string(body),
		)
	}
}

func (s *NorthWindService) GetTransferStatus(
	ctx context.Context,
	transferID string,
) (*dto.NorthWindTransferStatusResponse, error) {

	req, err := s.buildRequest(
		ctx,
		http.MethodGet,
		fmt.Sprintf(s.config.BaseUrl+"/external/transfers/%s", transferID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get transfer status: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var transferResp dto.NorthWindTransferStatusResponse
		if err := json.Unmarshal(body, &transferResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal transfer status response: %w", err)
		}

		s.logger.Info("Transfer status monitoring response", transferResp.TransferID, transferResp.Status)

		return &transferResp, nil

	case http.StatusNotFound:
		return nil, fmt.Errorf("transfer not found: %s", transferID)

	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		var errResp dto.NorthwindValidateAccountErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal error response: %w", err)
		}

		s.logger.Error("Northwind GetTransferStatus Error", "transfer_id", transferID, "error", errResp.Error.Message)

		return nil, fmt.Errorf(
			"northwind error (%d): %s",
			resp.StatusCode,
			errResp.Error.Message,
		)

	default:
		return nil, fmt.Errorf(
			"unexpected northwind status code (%d): %s",
			resp.StatusCode,
			string(body),
		)
	}
}

func (s *NorthWindService) Notify(data *models.Transfer) error {
	_, err := s.buildRequest(
		context.Background(),
		http.MethodGet,
		s.config.WebhookURL,
		data,
	)

	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}

	return nil
}
