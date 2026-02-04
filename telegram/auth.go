package telegram

import (
	"context"
	"fmt"

	"github.com/zelenin/go-tdlib/client"
)

func (c *Client) Authenticate(ctx context.Context) error {
	c.log.Info("Starting authentication flow")

	// Wait for authorization state
	currentState, err := c.client.GetAuthorizationState()
	if err != nil {
		c.log.Error("Failed to get authorization state", "error", err)
		return fmt.Errorf("failed to get auth state: %w", err)
	}

	c.log.Info("Current auth state", "state", currentState.AuthorizationStateType())

	switch currentState.AuthorizationStateType() {
	case client.TypeAuthorizationStateReady:
		c.log.Info("Already authorized")
		return nil

	case client.TypeAuthorizationStateWaitPhoneNumber:
		c.log.Info("Sending phone number", "phone", c.cfg.Phone)
		_, err := c.client.SetAuthenticationPhoneNumber(&client.SetAuthenticationPhoneNumberRequest{
			PhoneNumber: c.cfg.Phone,
		})
		if err != nil {
			c.log.Error("Failed to set phone number", "error", err)
			return err
		}

	case client.TypeAuthorizationStateWaitCode:
		c.log.Info("Waiting for authentication code")
		// Code will be entered via CLI interactor

	case client.TypeAuthorizationStateWaitPassword:
		c.log.Info("Waiting for 2FA password")
		// Password will be entered via CLI interactor
	}

	// Wait for ready state
	for {
		currentState, err = c.client.GetAuthorizationState()
		if err != nil {
			c.log.Error("Auth state check failed", "error", err)
			return err
		}

		if currentState.AuthorizationStateType() == client.TypeAuthorizationStateReady {
			c.log.Info("Authentication successful")
			return nil
		}
	}
}
