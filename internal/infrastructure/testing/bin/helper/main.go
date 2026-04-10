package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <subcommand> [args...]\n", os.Args[0])
		os.Exit(1)
	}

	subcommand := os.Args[1]

	switch subcommand {
	case "echo":
		fmt.Println(strings.Join(os.Args[2:], " "))

	case "cat":
		if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
			fmt.Fprintf(os.Stderr, "cat error: %v\n", err)
			os.Exit(1)
		}

	case "stderr":
		fmt.Fprintln(os.Stderr, strings.Join(os.Args[2:], " "))

	case "sleep":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "sleep: missing operand")
			os.Exit(1)
		}
		duration, err := strconv.ParseFloat(os.Args[2], 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sleep: invalid time interval %q: %v\n", os.Args[2], err)
			os.Exit(1)
		}
		time.Sleep(time.Duration(duration * float64(time.Second)))

	case "exit":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "exit: missing status")
			os.Exit(1)
		}
		code, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "exit: invalid status %q: %v\n", os.Args[2], err)
			os.Exit(1)
		}
		os.Exit(code)

	case "long-output":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "long-output: missing count")
			os.Exit(1)
		}
		n, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "long-output: invalid count %q: %v\n", os.Args[2], err)
			os.Exit(1)
		}
		bw := bufio.NewWriterSize(os.Stdout, 64*1024)
		for i := 0; i < n; i++ {
			if err := bw.WriteByte('a'); err != nil {
				fmt.Fprintf(os.Stderr, "long-output: write error: %v\n", err)
				os.Exit(1)
			}
		}
		if err := bw.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "long-output: flush error: %v\n", err)
			os.Exit(1)
		}

	case "grep":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "grep: missing pattern")
			os.Exit(1)
		}
		pattern := os.Args[2]
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, pattern) {
				fmt.Println(line)
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "grep: scanner error: %v\n", err)
			os.Exit(1)
		}

	case "deadlock-test":
		size := 128 * 1024
		data := make([]byte, size)
		for i := range data {
			data[i] = 'e'
		}
		os.Stderr.Write(data)
		fmt.Println("done")

	case "multi-line":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "multi-line: missing count")
			os.Exit(1)
		}
		n, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "multi-line: invalid count %q: %v\n", os.Args[2], err)
			os.Exit(1)
		}
		for i := 1; i <= n; i++ {
			fmt.Fprintf(os.Stdout, "STDOUT_LINE_%d\n", i)
			fmt.Fprintf(os.Stderr, "STDERR_LINE_%d\n", i)
		}

	case "printf":
		if len(os.Args) < 3 {
			os.Exit(0)
		}
		fmt.Print(os.Args[2])

	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", subcommand)
		os.Exit(1)
	}
}
