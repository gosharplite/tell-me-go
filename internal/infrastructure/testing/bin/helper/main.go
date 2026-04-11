package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var commands = map[string]func([]string){
	"echo":          handleEcho,
	"cat":           handleCat,
	"stderr":        handleStderr,
	"sleep":         handleSleep,
	"exit":          handleExit,
	"long-output":   handleLongOutput,
	"grep":          handleGrep,
	"deadlock-test": handleDeadlockTest,
	"multi-line":    handleMultiLine,
	"printf":        handlePrintf,
	"stress-output": handleStressOutput,
	"diff":          handleDiff,
	"printenv":      handlePrintenv,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <subcommand> [args...]\n", os.Args[0])
		os.Exit(1)
	}

	subcommand := os.Args[1]
	if handler, ok := commands[subcommand]; ok {
		handler(os.Args[2:])
		return
	}

	fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", subcommand)
	os.Exit(1)
}

func handleEcho(args []string) {
	fmt.Println(strings.Join(args, " "))
}

func handleCat(_ []string) {
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "cat error: %v\n", err)
		os.Exit(1)
	}
}

func handleStderr(args []string) {
	fmt.Fprintln(os.Stderr, strings.Join(args, " "))
}

func handleSleep(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "sleep: missing operand")
		os.Exit(1)
	}
	duration, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sleep: invalid time interval %q: %v\n", args[0], err)
		os.Exit(1)
	}
	time.Sleep(time.Duration(duration * float64(time.Second)))
}

func handleExit(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "exit: missing status")
		os.Exit(1)
	}
	code, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "exit: invalid status %q: %v\n", args[0], err)
		os.Exit(1)
	}
	os.Exit(code)
}

func handleLongOutput(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "long-output: missing count")
		os.Exit(1)
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "long-output: invalid count %q: %v\n", args[0], err)
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
}

func handleGrep(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "grep: missing pattern")
		os.Exit(1)
	}
	pattern := args[0]
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
}

func handleDeadlockTest(_ []string) {
	size := 128 * 1024
	data := make([]byte, size)
	for i := range data {
		data[i] = 'e'
	}
	_, _ = os.Stderr.Write(data)
	fmt.Println("done")
}

func handleMultiLine(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "multi-line: missing count")
		os.Exit(1)
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "multi-line: invalid count %q: %v\n", args[0], err)
		os.Exit(1)
	}
	for i := 1; i <= n; i++ {
		_, _ = fmt.Fprintf(os.Stdout, "STDOUT_LINE_%d\n", i)
		_, _ = fmt.Fprintf(os.Stderr, "STDERR_LINE_%d\n", i)
	}
}

func handlePrintf(args []string) {
	if len(args) < 1 {
		os.Exit(0)
	}
	fmt.Print(args[0])
}

func handleStressOutput(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "stress-output: missing count")
		os.Exit(1)
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "stress-output: invalid count %q: %v\n", args[0], err)
		os.Exit(1)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 1; i <= n; i++ {
			_, _ = fmt.Fprintf(os.Stdout, "STDOUT line %d - some unicode: 世界😀\n", i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 1; i <= n; i++ {
			_, _ = fmt.Fprintf(os.Stderr, "STDERR line %d - some unicode: 世😀界\n", i)
		}
	}()
	wg.Wait()
}

func handleDiff(args []string) {
	// Minimal diff simulation for tests
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "diff: missing operands")
		os.Exit(2)
	}
	// In tests, we often just want to see some difference
	// We'll read both files and print a simple unified-like diff if they differ
	f1, err := os.ReadFile(args[len(args)-2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff: %v\n", err)
		os.Exit(2)
	}
	f2, err := os.ReadFile(args[len(args)-1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff: %v\n", err)
		os.Exit(2)
	}
	if string(f1) == string(f2) {
		os.Exit(0)
	}
	// Print a dummy diff that satisfies strings.Contains checks in tests
	fmt.Printf("--- %s\n+++ %s\n", args[len(args)-2], args[len(args)-1])
	s1 := strings.Split(string(f1), "\n")
	s2 := strings.Split(string(f2), "\n")
	// Very simple line-by-line diff
	max := len(s1)
	if len(s2) > max {
		max = len(s2)
	}
	for i := 0; i < max; i++ {
		if i < len(s1) && i < len(s2) {
			if s1[i] != s2[i] {
				fmt.Printf("-%s\n+%s\n", s1[i], s2[i])
			} else {
				fmt.Printf(" %s\n", s1[i])
			}
		} else if i < len(s1) {
			fmt.Printf("-%s\n", s1[i])
		} else if i < len(s2) {
			fmt.Printf("+%s\n", s2[i])
		}
	}
	os.Exit(1)
}

func handlePrintenv(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "printenv: missing variable")
		os.Exit(1)
	}
	fmt.Println(os.Getenv(args[0]))
}
