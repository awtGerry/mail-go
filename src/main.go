package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	server = "imap.gmail.com"
	port   = "993"
)

func main() {
	email := os.Getenv("GMAIL_USER")
	passwd := os.Getenv("GMAIL_PASSWD")

	cmd := exec.Command("openssl", "s_client", "-connect", server+":"+port, "-crlf", "-quiet")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatalf("Error creando el conducto para stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("Error creando el conducto para stdout: %v", err)
	}

	// Arranca openssl
	if err := cmd.Start(); err != nil {
		log.Fatalf("Error inicializando openssl: %v", err)
	}

	fmt.Println("Conexion exitosa con Gmail")
	// Lector para stdout
	reader := bufio.NewReader(stdout)

	// Espera respuesta del servidor
	waitForResponse(reader)

	// Inicia los comandos IMAP
	fmt.Println("Iniciando sesion...")
	sendCommand(stdin, fmt.Sprintf("a1 LOGIN %s %s", email, passwd))
	resp := waitForResponse(reader)
	if !strings.Contains(resp, "a1 OK") {
		log.Fatalf("Login failed: %s", resp)
	}
	fmt.Println("Login successful")

	// List available mailboxes
	fmt.Println("\nListing mailboxes:")
	sendCommand(stdin, "a2 LIST \"\" \"*\"")
	resp = waitForResponse(reader)
	fmt.Println(resp)

	// Select INBOX
	fmt.Println("\nSelecting INBOX:")
	sendCommand(stdin, "a3 SELECT INBOX")
	resp = waitForResponse(reader)
	fmt.Println(resp)

	// Search for recent messages
	fmt.Println("\nSearching for recent messages:")
	sendCommand(stdin, "a4 SEARCH RECENT")
	resp = waitForResponse(reader)
	fmt.Println(resp)

	// Alternative search for all messages if no recent ones
	fmt.Println("\nSearching for all messages:")
	sendCommand(stdin, "a5 SEARCH ALL")
	resp = waitForResponse(reader)

	// Extract message IDs from search response
	msgIDs := extractMessageIDs(resp)

	if len(msgIDs) == 0 {
		fmt.Println("No messages found")
	} else {
		fmt.Printf("Found %d messages\n", len(msgIDs))

		// Fetch headers for the 5 most recent messages
		maxToFetch := 5
		if len(msgIDs) < maxToFetch {
			maxToFetch = len(msgIDs)
		}

		// Get the most recent messages (last N elements in the array)
		recentMsgIDs := msgIDs[len(msgIDs)-maxToFetch:]

		for _, msgID := range recentMsgIDs {
			fmt.Printf("\nFetching message %s:\n", msgID)
			sendCommand(stdin, fmt.Sprintf("a6 FETCH %s (FLAGS BODY[HEADER.FIELDS (FROM SUBJECT DATE)])", msgID))
			resp = waitForResponse(reader)
			fmt.Println(parseEmailHeaders(resp))

			// Fetch a small preview of the body
			fmt.Printf("Fetching body preview for message %s:\n", msgID)
			sendCommand(stdin, fmt.Sprintf("a7 FETCH %s BODY[TEXT]<0.500>", msgID))
			resp = waitForResponse(reader)
			bodyPreview := parseEmailBody(resp)
			fmt.Printf("Body preview: %s\n", bodyPreview)
		}
	}

	// Logout
	fmt.Println("\nLogging out...")
	sendCommand(stdin, "a8 LOGOUT")
	waitForResponse(reader)

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		log.Printf("Command finished with error: %v", err)
	}
}

// Funcion para mandar un comando IMAP al servidor
func sendCommand(stdin io.WriteCloser, command string) {
	_, err := io.WriteString(stdin, command+"\r\n")
	if err != nil {
		log.Fatalf("Error mandando el comando: %v", err)
	}

	time.Sleep(100 * time.Millisecond) // Asegura que el comando se intente ejecutar
}

// Funcion que lee la respuesta del servidor hasta que finaliza la etiqueta IMAP
func waitForResponse(reader *bufio.Reader) string {
	var response strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Fatalf("Error reading response: %v", err)
		}

		response.WriteString(line)

		// Checa si la respuesta del comando IMAP es la final
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "a") && (strings.Contains(trimmedLine, "OK") ||
			strings.Contains(trimmedLine, "NO") || strings.Contains(trimmedLine, "BAD")) {
			break
		}

		if strings.Contains(line, "* OK") && response.Len() < 200 {
			break
		}
	}

	time.Sleep(100 * time.Millisecond) // Delay para asegurar la respuesta

	return response.String()
}

// extractMessageIDs extracts message IDs from SEARCH response
func extractMessageIDs(searchResponse string) []string {
	var msgIDs []string

	// Find the line with "SEARCH" in it
	lines := strings.Split(searchResponse, "\n")
	for _, line := range lines {
		if strings.Contains(line, "* SEARCH") {
			// Extract the numbers after "* SEARCH"
			parts := strings.Split(line, "* SEARCH")
			if len(parts) < 2 {
				continue
			}

			// Split the remaining part by spaces to get individual IDs
			idParts := strings.Fields(parts[1])
			msgIDs = append(msgIDs, idParts...)
		}
	}

	return msgIDs
}

// parseEmailHeaders extracts readable header information from the FETCH response
func parseEmailHeaders(fetchResponse string) string {
	var result strings.Builder

	// Extract headers from the response
	fromLine := extractHeaderField(fetchResponse, "From:")
	subjectLine := extractHeaderField(fetchResponse, "Subject:")
	dateLine := extractHeaderField(fetchResponse, "Date:")

	if fromLine != "" {
		result.WriteString("From: " + strings.TrimSpace(fromLine) + "\n")
	}
	if subjectLine != "" {
		result.WriteString("Subject: " + strings.TrimSpace(subjectLine) + "\n")
	}
	if dateLine != "" {
		result.WriteString("Date: " + strings.TrimSpace(dateLine) + "\n")
	}

	return result.String()
}

// extractHeaderField finds and returns a specific header field from the response
func extractHeaderField(response, fieldName string) string {
	lines := strings.Split(response, "\n")
	for i, line := range lines {
		if strings.Contains(line, fieldName) {
			// Return the content after the field name
			fieldContent := strings.SplitN(line, fieldName, 2)
			if len(fieldContent) >= 2 {
				return fieldContent[1]
			}

			// Handle possible continuation lines
			if i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t")) {
				return fieldContent[1] + lines[i+1]
			}
		}
	}
	return ""
}

// parseEmailBody extracts the email body from the FETCH response
func parseEmailBody(fetchResponse string) string {
	// Look for the section between the first appearance of a line break after
	// the opening brace "{" and the closing line with the tag
	start := strings.Index(fetchResponse, "{")
	if start == -1 {
		return "Body not found"
	}

	// Find the next line break after the opening brace
	nlAfterBrace := strings.Index(fetchResponse[start:], "\n")
	if nlAfterBrace == -1 {
		return "Body format unexpected"
	}

	bodyStart := start + nlAfterBrace + 1

	// Find where the body ends (before the closing tag)
	bodyEnd := strings.LastIndex(fetchResponse, "a7 OK")
	if bodyEnd == -1 {
		// If we can't find the tag, just use the whole rest of the response
		return strings.TrimSpace(fetchResponse[bodyStart:])
	}

	// Find the last line break before the closing tag
	lastNL := strings.LastIndex(fetchResponse[:bodyEnd], "\n")
	if lastNL == -1 || lastNL < bodyStart {
		return strings.TrimSpace(fetchResponse[bodyStart:bodyEnd])
	}

	return strings.TrimSpace(fetchResponse[bodyStart:lastNL])
}
