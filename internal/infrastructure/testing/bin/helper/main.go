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
		_, _ = os.Stderr.Write(data)
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
			_, _ = fmt.Fprintf(os.Stdout, "STDOUT_LINE_%d\n", i)
			_, _ = fmt.Fprintf(os.Stderr, "STDERR_LINE_%d\n", i)
		}

	case "printf":
		if len(os.Args) < 3 {
			os.Exit(0)
		}
		fmt.Print(os.Args[2])

	case "stress-output":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "stress-output: missing count")
			os.Exit(1)
		}
		n, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "stress-output: invalid count %q: %v\n", os.Args[2], err)
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

	case "diff":
		// Minimal diff simulation for tests
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "diff: missing operands")
			os.Exit(2)
		}
		// In tests, we often just want to see some difference
		// We'll read both files and print a simple unified-like diff if they differ
		f1, err := os.ReadFile(os.Args[len(os.Args)-2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "diff: %v\n", err)
			os.Exit(2)
		}
		f2, err := os.ReadFile(os.Args[len(os.Args)-1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "diff: %v\n", err)
			os.Exit(2)
		}
		if string(f1) == string(f2) {
			os.Exit(0)
		}
		// Print a dummy diff that satisfies strings.Contains checks in tests
		fmt.Printf("--- %s\n+++ %s\n", os.Args[len(os.Args)-2], os.Args[len(os.Args)-1])
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

	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", subcommand)
		os.Exit(1)
	}
}
