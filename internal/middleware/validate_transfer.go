package middleware

// import (
// 	"array-assessment/internal/dto"
// 	"array-assessment/internal/errors"
// 	"array-assessment/internal/handlers"
// 	"array-assessment/internal/services"
// 	"bytes"
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"io"

// 	"github.com/labstack/echo/v4"
// )

// func ValidateTransfer(northWindService services.NorthWindServiceInterface) echo.MiddlewareFunc {
// 	return func(next echo.HandlerFunc) echo.HandlerFunc {
// 		return func(c echo.Context) error {

// 			bodyBytes, err := io.ReadAll(c.Request().Body)
// 			if err != nil {
// 				return handlers.SendError(c, errors.ValidationGeneral, errors.WithDetails("Invalid request body"))
// 			}

// 			c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

// 			var req dto.TransferRequest
// 			if err := json.Unmarshal(bodyBytes, &req); err != nil {
// 				return handlers.SendError(c, errors.ValidationGeneral, errors.WithDetails("Invalid request body"))
// 			}

// 			fmt.Println(req)

// 			res, err := northWindService.ValidateTransfer(context.TODO(), dto.NorthWindTransferValidationRequest{
// 				Amount:             req.Amount,
// 				Description:        req.Description,
// 				Currency:           req.Currency,
// 				ScheduledDate:      req.ScheduledDate,
// 				DestinationAccount: req.DestinationAccount,
// 			})

// 			if err != nil {
// 				return handlers.SendError(c, errors.NorthWindAccountError, errors.WithDetails("Error while authenticating northwind account"))
// 			}

// 			if !res.AccountExists {
// 				return handlers.SendError(c, errors.NorthWindAccountNotFound, errors.WithDetails("Northwind account not found"))
// 			}

// 			return next(c)
// 		}
// 	}
// }
