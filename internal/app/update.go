package app

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
)

func SelfUpdate() error {
	// In a real scenario, you'd fetch the latest version from a GitHub API or similar.
	// For this prototype, we'll assume a generic URL structure.
	const repoURL = "https://releases.hubpulse.space/agent/latest"
	
	binaryURL := fmt.Sprintf("%s/hubpulse-agent-%s-%s", repoURL, runtime.GOOS, runtime.GOARCH)
	
	fmt.Printf("Updating hubpulse-agent to latest version...\n")
	fmt.Printf("Downloading from %s\n", binaryURL)

	resp, err := http.Get(binaryURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download update: server returned %s", resp.Status)
	}

	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}

	// Create a temporary file for the new binary
	tempPath := executablePath + ".new"
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tempPath)

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to write update: %w", err)
	}
	tempFile.Close()

	// Verify the new binary (optional but recommended)
	cmd := exec.Command(tempPath, "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("downloaded binary is invalid: %w", err)
	}

	// Atomically replace the old binary
	if err := os.Rename(tempPath, executablePath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	fmt.Println("Successfully updated to latest version. Please restart the hubpulse-agent service.")
	return nil
}
