package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"fyne.io/fyne/v2/theme"
	"golang.org/x/net/proxy"
	"mime"
)

// Config structure for the configuration file
type Config struct {
	OnionAddress string `json:"onion_address"`
	Port         string `json:"port"`
}

// QuickMail structure for the application
type QuickMail struct {
	app         fyne.App
	window      fyne.Window
	textArea    *widget.Entry
	config      *Config
	isDarkTheme bool
}

// loadConfig loads the configuration from quickmail.json
func loadConfig() (*Config, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	
	configPath := filepath.Join(filepath.Dir(exePath), "quickmail.json")
	
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("could not read config file: %w", err)
	}
	
	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("could not parse config file: %w", err)
	}
	
	return &config, nil
}

// encodeMIMESubject encodes the subject with MIME base64 and folding
func encodeMIMESubject(input string) string {
	if input == "" {
		return ""
	}
	
	// First get the complete encoded string
	encoded := mime.BEncoding.Encode("UTF-8", input)
	
	// Split at "?=" and handle each part separately
	parts := strings.Split(encoded, "?=")
	if len(parts) <= 1 {
		return encoded
	}
	
	var result string
	for i, part := range parts[:len(parts)-1] {
		if i > 0 {
			result += ""
		}
		result += part + "?=\n"
	}
	result += parts[len(parts)-1]
	
	return strings.TrimSuffix(result, "\n")
}

// sendMail sends the message via Tor like ocsend.go
func (q *QuickMail) sendMail() {
	if q.config == nil {
		q.showError("Configuration not loaded")
		return
	}
	
	message := q.textArea.Text
	if strings.TrimSpace(message) == "" {
		q.showError("Message is empty")
		return
	}
	
	serverAddress := q.config.OnionAddress
	if q.config.Port != "" {
		serverAddress += ":" + q.config.Port
	}
	
	if !strings.HasPrefix(serverAddress, "http://") && !strings.HasPrefix(serverAddress, "https://") {
		serverAddress = "http://" + serverAddress
	}
	serverURL := serverAddress + "/upload"
	
	go func() {
		err := q.uploadMessage(serverURL, message)
		if err != nil {
			q.showError(fmt.Sprintf("Send error: %v", err))
		} else {
			q.showSuccess("Message sent successfully!")
		}
	}()
}

// uploadMessage uploads the message via Tor
func (q *QuickMail) uploadMessage(serverURL, message string) error {
	startTime := time.Now()

	data := []byte(message)

	dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:9050", nil, proxy.Direct)
	if err != nil {
		return fmt.Errorf("can't connect to Tor proxy: %w", err)
	}
	
	httpTransport := &http.Transport{
		Dial: dialer.Dial,
	}
	client := &http.Client{
		Transport: httpTransport,
		Timeout:   30 * time.Second,
	}

	request, err := http.NewRequest("POST", serverURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	request.Header.Set("Content-Type", "application/octet-stream")

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	responseBody, _ := io.ReadAll(response.Body)

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s, body: %s", response.Status, string(responseBody))
	}

	elapsedTime := time.Since(startTime)
	fmt.Printf("Message sent successfully! Elapsed Time: %s\n", q.formatDuration(elapsedTime))

	return nil
}

func (q *QuickMail) formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// clearContent safely clears the text area and clipboard
func (q *QuickMail) clearContent() {
	q.textArea.SetText("")
	if q.window.Clipboard() != nil {
		q.window.Clipboard().SetContent("")
	}
	// Additional secure clearing could be implemented here with memguard if needed
}

// toggleTheme switches between dark and light theme
func (q *QuickMail) toggleTheme() {
	if q.isDarkTheme {
		q.app.Settings().SetTheme(theme.LightTheme())
		q.isDarkTheme = false
	} else {
		q.app.Settings().SetTheme(theme.DarkTheme())
		q.isDarkTheme = true
	}
	q.window.Content().Refresh()
}

// showError shows an error dialog
func (q *QuickMail) showError(message string) {
	dialog.ShowInformation("Error", message, q.window)
}

// showSuccess shows a success dialog
func (q *QuickMail) showSuccess(message string) {
	dialog.ShowInformation("Success", message, q.window)
}

// showSubjectDialog shows a dialog to enter the subject and encodes it
func (q *QuickMail) showSubjectDialog() {
	subjectEntry := widget.NewEntry()
	subjectEntry.PlaceHolder = "Enter subject here..."
	
	subjectDialog := dialog.NewForm(
		"Enter Subject",
		"Encode",
		"Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Subject:", subjectEntry),
		},
		func(confirmed bool) {
			if confirmed && subjectEntry.Text != "" {
				encodedSubject := encodeMIMESubject(subjectEntry.Text) + "\n"
				
				// Get current text and cursor position
				currentText := q.textArea.Text
				
				// For widget.Entry, we can use CursorPosition
				cursorPos := q.textArea.CursorColumn
				row := q.textArea.CursorRow
				
				// Calculate actual cursor position in the full text
				// We need to account for multi-line text
				lines := strings.Split(currentText, "\n")
				actualPos := 0
				
				// Calculate position up to the current row
				for i := 0; i < row; i++ {
					if i < len(lines) {
						actualPos += len(lines[i]) + 1 // +1 for newline
					}
				}
				
				// Add the column position within the current row
				if row < len(lines) {
					if cursorPos > len(lines[row]) {
						cursorPos = len(lines[row])
					}
					actualPos += cursorPos
				} else {
					// If cursor is beyond existing lines, put at end
					actualPos = len(currentText)
				}
				
				// Insert at the calculated position
				newText := currentText[:actualPos] + encodedSubject + currentText[actualPos:]
				q.textArea.SetText(newText)
				
				// Move cursor to end of inserted text
				newCursorPos := actualPos + len(encodedSubject)
				// We need to calculate new row and column
				newLines := strings.Split(newText[:newCursorPos], "\n")
				q.textArea.CursorRow = len(newLines) - 1
				q.textArea.CursorColumn = len(newLines[len(newLines)-1])
			}
		},
		q.window,
	)
	
	subjectDialog.Show()
	subjectDialog.Resize(fyne.NewSize(460, 150))
}

func main() {
	myApp := app.New()
	window := myApp.NewWindow("Quick Mail")

	// Load configuration
	config, err := loadConfig()
	if err != nil {
		fmt.Printf("Warning: Could not load config: %v\n", err)
	}

	// Create QuickMail instance
	quickMail := &QuickMail{
		app:         myApp,
		window:      window,
		config:      config,
		isDarkTheme: true,
	}

	// Set initial theme
	myApp.Settings().SetTheme(theme.DarkTheme())

	// Create text area with mono font
	textArea := widget.NewMultiLineEntry()
	textArea.TextStyle = fyne.TextStyle{Monospace: true}
	textArea.Wrapping = fyne.TextWrapWord
	textArea.MultiLine = true
	textArea.PlaceHolder = "Enter your message here..."

	quickMail.textArea = textArea

	// Create theme switch button
	themeSwitch := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), quickMail.toggleTheme)
	themeSwitch.Importance = widget.LowImportance

	// Create top bar
	topBar := container.NewHBox(
		layout.NewSpacer(),
		themeSwitch,
	)

	// Create centered buttons
	mimeButton := widget.NewButton("MIME", func() {
		quickMail.showSubjectDialog()
	})

	sendButton := widget.NewButton("Send", func() {
		quickMail.sendMail()
	})

	clearButton := widget.NewButton("Clear", func() {
		quickMail.clearContent()
	})

	// Center the buttons
	buttons := container.NewHBox(
		layout.NewSpacer(),
		mimeButton,
		sendButton,
		clearButton,
		layout.NewSpacer(),
	)

	// Create main content
	content := container.NewBorder(
		container.NewVBox(
			topBar,
			widget.NewSeparator(),
		),
		buttons,
		nil,
		nil,
		container.NewScroll(textArea),
	)

	window.SetContent(content)
	window.Resize(fyne.NewSize(800, 600))
	window.ShowAndRun()
}
