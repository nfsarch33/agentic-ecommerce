package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPingRedisSelectsConfiguredDatabaseAndPings(t *testing.T) {
	t.Parallel()

	commands := make(chan []string, 2)
	addr := startReadinessRedisServer(t, func(cmd []string) string {
		commands <- cmd
		switch cmd[0] {
		case "SELECT":
			return "+OK\r\n"
		case "PING":
			return "+PONG\r\n"
		default:
			return "-ERR unexpected command\r\n"
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := pingRedis(ctx, addr, "3"); err != nil {
		t.Fatalf("pingRedis: %v", err)
	}
	if got := <-commands; len(got) != 2 || got[0] != "SELECT" || got[1] != "3" {
		t.Fatalf("select command = %#v", got)
	}
	if got := <-commands; len(got) != 1 || got[0] != "PING" {
		t.Fatalf("ping command = %#v", got)
	}
}

func TestPingRedisRejectsInvalidDatabaseBeforeCommands(t *testing.T) {
	t.Parallel()

	addr := startReadinessRedisServer(t, func([]string) string {
		t.Fatal("server should not receive commands for invalid db")
		return ""
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := pingRedis(ctx, addr, "not-a-number"); err == nil || !strings.Contains(err.Error(), "redis db") {
		t.Fatalf("pingRedis error = %v, want redis db parse error", err)
	}
}

func TestRedisCommandEncodesRESPArrays(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rw := bufio.NewReadWriter(bufio.NewReader(strings.NewReader("")), bufio.NewWriter(&buf))
	if err := redisCommand(rw, "SELECT", "4"); err != nil {
		t.Fatalf("redisCommand: %v", err)
	}
	if got, want := buf.String(), "*2\r\n$6\r\nSELECT\r\n$1\r\n4\r\n"; got != want {
		t.Fatalf("RESP command = %q, want %q", got, want)
	}
}

func TestReadRedisSimpleResponseHandlesServerAndProtocolErrors(t *testing.T) {
	t.Parallel()

	if err := readRedisSimpleResponse(bufio.NewReader(strings.NewReader("+PONG\r\n")), "PONG"); err != nil {
		t.Fatalf("simple response: %v", err)
	}
	if err := readRedisSimpleResponse(bufio.NewReader(strings.NewReader("-NOAUTH Authentication required\r\n")), "PONG"); err == nil || !strings.Contains(err.Error(), "NOAUTH") {
		t.Fatalf("server error = %v, want NOAUTH", err)
	}
	if err := readRedisSimpleResponse(bufio.NewReader(strings.NewReader("+OK\r\n")), "PONG"); err == nil || !strings.Contains(err.Error(), "want +PONG") {
		t.Fatalf("protocol error = %v, want +PONG mismatch", err)
	}
}

func startReadinessRedisServer(t *testing.T, respond func([]string) string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			cmd, err := readReadinessRedisCommand(reader)
			if err != nil {
				return
			}
			if _, err := conn.Write([]byte(respond(cmd))); err != nil {
				return
			}
		}
	}()

	return listener.Addr().String()
}

func readReadinessRedisCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(line), "*"))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		sizeLine, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(sizeLine), "$"))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, err
		}
		out = append(out, string(buf[:size]))
	}
	return out, nil
}
