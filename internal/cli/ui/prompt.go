package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	promptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00")).
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFA500")).
			Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00BFFF"))
)

// Prompt displays a prompt and returns user input
func Prompt(message string) (string, error) {
	fmt.Print(promptStyle.Render(message + ": "))
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

// PromptYesNo displays a yes/no prompt and returns true for yes
func PromptYesNo(message string, defaultYes bool) bool {
	defaultText := "y/N"
	if defaultYes {
		defaultText = "Y/n"
	}

	fmt.Print(promptStyle.Render(fmt.Sprintf("%s [%s]: ", message, defaultText)))
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return defaultYes
	}

	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return defaultYes
	}

	return input == "y" || input == "yes"
}

// PromptSelect displays a selection prompt
func PromptSelect(message string, options []string) (int, error) {
	fmt.Println(promptStyle.Render(message))
	for i, option := range options {
		fmt.Printf("  %d. %s\n", i+1, option)
	}

	fmt.Print(promptStyle.Render("Select [1-" + fmt.Sprintf("%d", len(options)) + "]: "))
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return -1, err
	}

	var selection int
	_, err = fmt.Sscanf(strings.TrimSpace(input), "%d", &selection)
	if err != nil || selection < 1 || selection > len(options) {
		return -1, fmt.Errorf("invalid selection")
	}

	return selection - 1, nil
}

// Error prints an error message
func Error(message string) {
	fmt.Println(errorStyle.Render("✗ " + message))
}

// Success prints a success message
func Success(message string) {
	fmt.Println(successStyle.Render("✓ " + message))
}

// Warning prints a warning message
func Warning(message string) {
	fmt.Println(warningStyle.Render("⚠ " + message))
}

// Info prints an info message
func Info(message string) {
	fmt.Println(infoStyle.Render("ℹ " + message))
}

// Header prints a styled header
func Header(message string) {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#7D56F4")).
		Padding(0, 1)
	fmt.Println(style.Render(message))
}

// Divider prints a divider line
func Divider() {
	fmt.Println(strings.Repeat("─", 80))
}
