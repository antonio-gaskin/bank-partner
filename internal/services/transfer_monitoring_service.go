package services

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"array-assessment/internal/models"
	"array-assessment/internal/repositories"
)

type TransferMonitoringService struct {
	transferRepo     repositories.TransferRepositoryInterface
	accountRepo      repositories.AccountRepositoryInterface
	transactionRepo  repositories.TransactionRepositoryInterface
	auditRepo        repositories.AuditLogRepositoryInterface
	metrics          MetricsRecorderInterface
	northWindService NorthWindServiceInterface
	logger           *slog.Logger
}

func NewTransferMonitoringService(
	transferRepo repositories.TransferRepositoryInterface,
	accountRepo repositories.AccountRepositoryInterface,
	transactionRepo repositories.TransactionRepositoryInterface,
	auditRepo repositories.AuditLogRepositoryInterface,
	metrics MetricsRecorderInterface,
	northWindService NorthWindServiceInterface,
	logger *slog.Logger,
) *TransferMonitoringService {
	return &TransferMonitoringService{
		transferRepo:     transferRepo,
		accountRepo:      accountRepo,
		transactionRepo:  transactionRepo,
		auditRepo:        auditRepo,
		metrics:          metrics,
		northWindService: northWindService,
		logger:           logger,
	}
}

func (s *TransferMonitoringService) StartProcessing(processingCtx context.Context) {

	ticker := time.NewTicker(5 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	var wg sync.WaitGroup

	for {
		select {
		case <-processingCtx.Done():
			s.logger.Info("Transfer monitoring stopped")
			return
		case <-ticker.C:
			transfers, err := s.transferRepo.FindPendingTransfers()

			if err != nil {
				s.logger.Error("Transfer monitoring error", "error", err)
			}

			for _, item := range transfers {
				wg.Add(1)
				go s.MonitorPendingTransfers(item)
			}
		}
	}
}

// MonitorPendingTransfers checks and updates the status of pending transfers
func (s *TransferMonitoringService) MonitorPendingTransfers(transfer *models.Transfer) error {
	ctx, _ := context.WithCancel(context.Background())

	resp, err := s.northWindService.GetTransferStatus(context.Background(), transfer.TransferId)

	if err != nil {
		return err
	}

	if resp.Status == models.TransferStatusCompleted {
		s.checkAndUpdateTransferStatus(ctx, transfer)
	}

	return nil
}

// checkAndUpdateTransferStatus checks the status of a single transfer and updates it if completed
func (s *TransferMonitoringService) checkAndUpdateTransferStatus(ctx context.Context, transfer *models.Transfer) error {
	s.logger.Info("Checking transfer status", "transfer_id", transfer.ID, "status", transfer.Status)

	if s.shouldNotifyTransfer(transfer) {
		s.northWindService.Notify(transfer)
	}

	// For this implementation, we'll simulate that transfers complete after some time
	// In a real implementation, this would check with an external service
	return s.finishTransfer(ctx, transfer)
}

// shouldCompleteTransfer determines if a transfer should be completed
// This is a simplified implementation - in reality, this would check external status
func (s *TransferMonitoringService) shouldNotifyTransfer(transfer *models.Transfer) bool {
	// Complete transfers that are more than 1 minute old (simulating external processing)
	return time.Since(transfer.CreatedAt) <= time.Minute*60
}

// completeTransfer completes a pending transfer by creating the necessary transactions
func (s *TransferMonitoringService) finishTransfer(ctx context.Context, transfer *models.Transfer) error {
	s.logger.Info("Finishing transfer", "transfer_id", transfer.ID)

	// Get the accounts
	fromAccount, err := s.accountRepo.GetByID(transfer.FromAccountID)
	if err != nil {
		return err
	}

	toAccount, err := s.accountRepo.GetByID(transfer.ToAccountID)
	if err != nil {
		return err
	}

	// Check if accounts are still active
	if !fromAccount.IsActive() || !toAccount.IsActive() {
		return s.failTransfer(transfer, "One or more accounts are no longer active")
	}

	// Check if sender has sufficient funds
	if fromAccount.Balance.LessThan(transfer.Amount) {
		return s.failTransfer(transfer, "Insufficient funds")
	}

	// Create the debit and credit transactions
	fromDescription := "Transfer completion: " + transfer.Description
	toDescription := "Transfer completion: " + transfer.Description

	debitTxID, creditTxID, err := s.accountRepo.ExecuteAtomicTransfer(
		fromAccount.ID,
		toAccount.ID,
		transfer.Amount,
		fromDescription,
		toDescription,
		models.GenerateTransactionReference(),
		transfer.Status,
	)
	if err != nil {
		return s.failTransfer(transfer, "Failed to execute transfer: "+err.Error())
	}

	// Update transfer with success
	transfer.Complete(debitTxID, creditTxID, transfer.Status)
	if err := s.transferRepo.Update(transfer); err != nil {
		s.logger.Error("Failed to update transfer status", "error", err)
		return err
	}

	// Log audit event
	if err := s.auditRepo.Create(&models.AuditLog{
		UserID:     &fromAccount.UserID,
		Action:     "transfer.completed",
		Resource:   "transfer",
		ResourceID: transfer.ID.String(),
		IPAddress:  "system",
		UserAgent:  "transfer-monitor",
		Metadata: models.JSONBMap{
			"from_account":    fromAccount.AccountNumber,
			"to_account":      toAccount.AccountNumber,
			"amount":          transfer.Amount.String(),
			"transfer_id":     transfer.ID.String(),
			"idempotency_key": transfer.IdempotencyKey,
		},
	}); err != nil {
		s.logger.Error("Failed to create audit log", "error", err)
	}

	s.logger.Info("Transfer completed successfully", "transfer_id", transfer.ID)
	return nil
}

// failTransfer marks a transfer as failed
func (s *TransferMonitoringService) failTransfer(transfer *models.Transfer, reason string) error {
	transfer.Fail(reason)
	if err := s.transferRepo.Update(transfer); err != nil {
		s.logger.Error("Failed to update transfer status to failed", "error", err)
		return err
	}

	s.logger.Info("Transfer marked as failed", "transfer_id", transfer.ID, "reason", reason)
	return nil
}
