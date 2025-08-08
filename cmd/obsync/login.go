package main

import (
    "context"
    "fmt"
    "os"
    "syscall"
    "time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/TheMichaelB/obsync/internal/services/totp"
    creds "github.com/TheMichaelB/obsync/internal/creds"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with the Obsync service",
	Long:  `Login stores authentication credentials for future sync operations.`,
	Example: `  obsync login --email user@example.com
  obsync login --email user@example.com --totp 123456`,
	RunE: runLogin,
}

var (
	loginEmail    string
	loginPassword string
	loginTOTP     string
)

func init() {
	rootCmd.AddCommand(loginCmd)

	loginCmd.Flags().StringVarP(&loginEmail, "email", "e", "",
		"Email address (required)")
	loginCmd.Flags().StringVarP(&loginPassword, "password", "p", "",
		"Password (will prompt if not provided)")
	loginCmd.Flags().StringVar(&loginTOTP, "totp", "",
		"TOTP code for 2FA (auto-generated from config if not provided)")

	_ = loginCmd.MarkFlagRequired("email")
}

func runLogin(cmd *cobra.Command, args []string) error {
    ctx := context.Background()

    // If a combined credentials file is configured and flags not fully provided, use it
    if cfg != nil && cfg.Auth.CombinedCredentialsFile != "" {
        if c, err := creds.LoadFromFile(cfg.Auth.CombinedCredentialsFile); err == nil {
            if loginEmail == "" && c.Auth.Email != "" {
                loginEmail = c.Auth.Email
            }
            if loginPassword == "" && c.Auth.Password != "" {
                loginPassword = c.Auth.Password
            }
            if loginTOTP == "" && c.Auth.TOTPSecret != "" {
                loginTOTP = c.Auth.TOTPSecret
            }
        }
    }

    // Get password if not provided
    if loginPassword == "" {
        var err error
        loginPassword, err = promptPassword("Password: ")
        if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
	}

	// Generate TOTP code if not provided but secret is available
    if loginTOTP != "" && len(loginTOTP) != 6 {
        // If loginTOTP is provided but not a 6-digit code, treat it as a secret
        totpService := totp.NewService()
		
		// Validate the secret first
		if err := totpService.IsValidSecret(loginTOTP); err != nil {
			if !jsonOutput {
				printError("Invalid TOTP secret: %v", err)
			}
			return fmt.Errorf("invalid totp_secret: %w", err)
		}

		// Generate current TOTP code
		code, err := totpService.GenerateCode(loginTOTP)
		if err != nil {
			if !jsonOutput {
				printError("Failed to generate TOTP code: %v", err)
			}
			return fmt.Errorf("generate totp: %w", err)
		}

		loginTOTP = code
		
		if !jsonOutput {
			// Show time remaining in current window for user awareness
			_, remaining := totpService.GetTimeWindow()
			printInfo("Generated TOTP code: %s (valid for %v)", code, remaining.Round(time.Second))
		}
	}

	// Attempt login
	if err := apiClient.Auth.Login(ctx, loginEmail, loginPassword, loginTOTP); err != nil {
		if jsonOutput {
			printJSON(map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		} else {
			printError("Login failed: %v", err)
		}
		return err
	}

	// Success
	if jsonOutput {
		printJSON(map[string]interface{}{
			"success": true,
			"email":   loginEmail,
		})
	} else {
		printSuccess("Successfully logged in as %s", loginEmail)
	}

	return nil
}

func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)

	// Read password without echo
	password, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr) // New line after password

	if err != nil {
		return "", err
	}

	return string(password), nil
}
