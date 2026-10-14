package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	remoteHost = "82.29.54.7:22"
	remoteUser = "root"
	remotePass = "recoveryhappens"
)

func main() {
	uploadSrc := flag.String("upload-src", "", "local file to upload")
	uploadDst := flag.String("upload-dst", "", "remote path to upload to")
	flag.Parse()

	cmd := ""
	if flag.NArg() > 0 {
		cmd = flag.Arg(0)
	}

	config := &ssh.ClientConfig{
		User: remoteUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(remotePass),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", remoteHost, config)
	if err != nil {
		log.Fatalf("Failed to dial SSH: %v", err)
	}
	defer client.Close()

	if *uploadSrc != "" && *uploadDst != "" {
		if err := uploadFile(client, *uploadSrc, *uploadDst); err != nil {
			log.Fatalf("Upload failed: %v", err)
		}
		fmt.Printf("Uploaded %s to %s successfully\n", *uploadSrc, *uploadDst)
	}

	if cmd != "" {
		session, err := client.NewSession()
		if err != nil {
			log.Fatalf("Failed to create session: %v", err)
		}
		defer session.Close()

		var stdoutBuf, stderrBuf bytes.Buffer
		session.Stdout = &stdoutBuf
		session.Stderr = &stderrBuf

		err = session.Run(cmd)
		fmt.Printf("--- STDOUT ---\n%s\n", stdoutBuf.String())
		if stderrBuf.Len() > 0 {
			fmt.Printf("--- STDERR ---\n%s\n", stderrBuf.String())
		}
		if err != nil {
			log.Fatalf("Command error: %v", err)
		}
	}
}

func uploadFile(client *ssh.Client, localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local: %w", err)
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	filename := filepath.Base(remotePath)
	dir := filepath.Dir(remotePath)

	go func() {
		defer stdin.Close()
		// SCP protocol write command
		fmt.Fprintf(stdin, "C0755 %d %s\n", len(data), filename)
		stdin.Write(data)
		fmt.Fprint(stdin, "\x00")
	}()

	return session.Run(fmt.Sprintf("scp -tr %s", dir))
}
