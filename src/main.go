package main

import (
	"bufio"
	"fmt"
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

func fetchEmail() error {
	email := os.Getenv("GMAIL_USER")
	passwd := os.Getenv("GMAIL_PASSWD")

	// Verificar que las variables de entorno estén configuradas
	if email == "" || passwd == "" {
		return fmt.Errorf("GMAIL_USER y GMAIL_PASSWD deben estar configuradas")
	}

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

	time.Sleep(2 * time.Second)

	fmt.Println("Conexion exitosa con Gmail")

	// Lector para stdout
	reader := bufio.NewReader(stdout)
	writer := bufio.NewWriter(stdin)

	// Funcion para mandar un comando a openssl y leer la respuesta
	sendCommand := func(command string) ([]string, error) {
		fmt.Printf("Enviado: %s\n", command)
		if _, err := writer.WriteString(command + "\r\n"); err != nil {
			return nil, err
		}
		if err := writer.Flush(); err != nil {
			return nil, err
		}

		var responses []string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return responses, err
			}
			line = strings.TrimSpace(line)
			fmt.Printf("Recibe: %s\n", line)
			responses = append(responses, line)

			// Final de la respuesta
			if strings.Contains(line, "OK") || strings.Contains(line, "BAD") || strings.Contains(line, "NO") {
				break
			}
		}
		return responses, nil
	}

	// Login
	if _, err := sendCommand(fmt.Sprintf(`A001 LOGIN "%s" "%s"`, email, passwd)); err != nil {
		return fmt.Errorf("Fallo en inicio de sesion: %v", err)
	}

	// INBOX
	if _, err := sendCommand("A002 SELECT INBOX"); err != nil {
		return fmt.Errorf("Fallo intento de seleccionar inbox: %v", err)
	}

	// Si tenemos inbox conseguir emails recientes
	responses, err := sendCommand("A003 FETCH 1:5 (ENVELOPE)")
	if err != nil {
		return fmt.Errorf("Busqueda fallida: %v", err)
	}

	fmt.Println("\n--- Email ---")
	for _, response := range responses {
		if strings.Contains(response, "ENVELOPE") {
			fmt.Println(response)
		}
	}

	// Logout
	sendCommand("A004 LOGOUT")

	return cmd.Wait()
}

func main() {
	if err := fetchEmail(); err != nil {
		fmt.Printf("OpenSSL approach failed: %v\n", err)
	}
}
