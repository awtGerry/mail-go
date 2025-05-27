package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type EmailData struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	From    string `json:"from"`
	To      string `json:"to"`
	Date    string `json:"date"`
	Body    string `json:"body"`
}

type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURL  string `json:"redirect_url"`
}

// Load OAuth2 configuration from credentials.json
func loadConfig() (*Config, error) {
	data, err := os.ReadFile("credentials.json")
	if err != nil {
		return nil, fmt.Errorf("unable to read credentials file: %v", err)
	}

	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("unable to parse credentials: %v", err)
	}

	return &config, nil
}

// Get OAuth2 token from web
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.Background(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Save token to file
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// Load token from file
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Get OAuth2 client
func getClient(config *oauth2.Config) *http.Client {
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Fetch emails from Gmail
func fetchEmails(service *gmail.Service, maxResults int64) ([]*EmailData, error) {
	call := service.Users.Messages.List("me").MaxResults(maxResults)
	messages, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve messages: %v", err)
	}

	var emails []*EmailData
	for _, message := range messages.Messages {
		msg, err := service.Users.Messages.Get("me", message.Id).Format("full").Do()
		if err != nil {
			log.Printf("Unable to retrieve message %s: %v", message.Id, err)
			continue
		}

		email := &EmailData{
			ID: msg.Id,
		}

		// Extract headers
		for _, header := range msg.Payload.Headers {
			switch header.Name {
			case "Subject":
				email.Subject = header.Value
			case "From":
				email.From = header.Value
			case "To":
				email.To = header.Value
			case "Date":
				email.Date = header.Value
			}
		}

		// Extract body
		email.Body = extractBody(msg.Payload)

		emails = append(emails, email)
	}

	return emails, nil
}

// Extract email body from payload
func extractBody(payload *gmail.MessagePart) string {
	var body string

	if payload.Body != nil && payload.Body.Data != "" {
		decoded, err := decodeBase64URL(payload.Body.Data)
		if err == nil {
			body = string(decoded)
		}
	}

	// Check parts for multipart messages
	for _, part := range payload.Parts {
		if part.MimeType == "text/plain" || part.MimeType == "text/html" {
			if part.Body != nil && part.Body.Data != "" {
				decoded, err := decodeBase64URL(part.Body.Data)
				if err == nil {
					body += string(decoded)
				}
			}
		}
		// Recursively check nested parts
		if len(part.Parts) > 0 {
			body += extractBody(part)
		}
	}

	return body
}

func decodeBase64URL(data string) ([]byte, error) {
	missing := len(data) % 4
	if missing != 0 {
		data += strings.Repeat("=", 4-missing)
	}

	// Replace URL-safe characters
	data = strings.ReplaceAll(data, "-", "+")
	data = strings.ReplaceAll(data, "_", "/")

	cmd := exec.Command("echo", data)
	cmd2 := exec.Command("base64", "-d")

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	cmd2.Stdin = pipe

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	result, err := cmd2.Output()
	if err != nil {
		return nil, err
	}

	cmd.Wait()

	return result, nil
}

func encryptEmail(email *EmailData, password string) (string, error) {
	emailJSON, err := json.Marshal(email)
	if err != nil {
		return "", fmt.Errorf("failed to marshal email: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "email_*.json")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(emailJSON)
	if err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// openssl AES
	cmd := exec.Command("openssl", "enc", "-aes-256-cbc", "-salt", "-in", tmpFile.Name(), "-pass", "pass:"+password)

	encryptedData, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("encryption failed: %v", err)
	}

	return string(encryptedData), nil
}

func matchEmailWithKey(email *EmailData, key string) bool {
	return strings.Contains(strings.ToLower(email.Subject), strings.ToLower(key)) ||
		strings.Contains(strings.ToLower(email.From), strings.ToLower(key)) ||
		strings.Contains(strings.ToLower(email.Body), strings.ToLower(key))
}

func main() {
	// Load OAuth2 configuration
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Setup OAuth2
	oauthConfig := &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  "urn:ietf:wg:oauth:2.0:oob", // Use OOB flow
		Scopes:       []string{gmail.GmailReadonlyScope},
		Endpoint:     google.Endpoint,
	}

	// Get authenticated client
	client := getClient(oauthConfig)

	// Create Gmail service
	service, err := gmail.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to create Gmail service: %v", err)
	}

	// Fetch emails
	fmt.Println("Fetching emails...")
	emails, err := fetchEmails(service, 10) // Fetch last 10 emails
	if err != nil {
		log.Fatalf("Error fetching emails: %v", err)
	}

	fmt.Printf("mails encontrados: %d\n", len(emails))

	encryptionKey := "ceti123"
	password := "pass123"

	var encryptedEmails []string
	for _, email := range emails {
		if matchEmailWithKey(email, encryptionKey) {
			fmt.Printf("Correo coincide con la clave '%s', encriptando...\n", encryptionKey)

			encrypted, err := encryptEmail(email, password)
			if err != nil {
				log.Printf("Failed to encrypt email: %v", err)
				continue
			}

			encryptedEmails = append(encryptedEmails, encrypted)
			fmt.Println("email encrypted")
		} else {
			fmt.Println("No match")
		}
	}

	fmt.Printf("\nSe encriptaron un total de: %d correos\n", len(encryptedEmails))

	// TODO: Reemplazar el correo original con el encriptado
	if len(encryptedEmails) > 0 {
		// timestamp := time.Now().Format("20060102_150405")
		filename := fmt.Sprintf("encrypted_emails.txt")

		file, err := os.Create(filename)
		if err != nil {
			log.Printf("error creando archivo %v", err)
			return
		}
		defer file.Close()

		for i, encrypted := range encryptedEmails {
			file.WriteString(fmt.Sprintf("=== Correo num %d ===\n", i+1))
			file.WriteString(encrypted)
			file.WriteString("\n\n")
		}

		fmt.Printf("%s creado exitosamente\n", filename)
	}
}
