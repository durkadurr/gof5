#!/usr/bin/env bash

PID_FILE="/tmp/gof5/$(whoami).pid"
LOG_FILE="/tmp/gof5/$(whoami).log"
CONFIG="/etc/gof5/config.yaml"
SERVER=""
USERNAME=""

start() {
	# Check if already running
	if [[ -f "$PID_FILE" ]]; then
		PID=$(cat "$PID_FILE" 2>/dev/null)
		if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
			echo "gof5 is already running (PID: $PID)"
			exit 1
		fi
	fi

	# Read password from environment variable
	if [[ -z "$GOF5_PASSWORD" ]]; then
		echo "Error: GOF5_PASSWORD environment variable is required"
		exit 1
	fi

	# Ensure log directory exists
	mkdir -p "$(dirname "$LOG_FILE")"

	echo "Starting gof5..."
	# Start in foreground first to see any immediate errors, then it will daemonize itself
	GOF5_PASSWORD="$GOF5_PASSWORD" gof5 --config "$CONFIG" --close-session --username ${USERNAME} --server ${SERVER} 2>&1
	GOF5_EXIT=$?

	# Give the daemon a moment to start and write PID file
	sleep 2

	# Check if it started successfully
	if [[ -f "$PID_FILE" ]]; then
		PID=$(cat "$PID_FILE" 2>/dev/null)
		if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
			echo "gof5 started successfully (PID: $PID)"
		else
			echo "gof5 failed to start (check logs: $LOG_FILE)"
			exit 1
		fi
	else
		echo "gof5 failed to start - no PID file created"
		exit 1
	fi

	exit 0
}

stop() {
	if [[ ! -f "$PID_FILE" ]]; then
		echo "gof5 is not running (PID file not found)"
		exit 1
	fi

	PID=$(cat "$PID_FILE" 2>/dev/null)
	if [[ -z "$PID" ]]; then
		echo "Invalid PID file"
		rm -f "$PID_FILE"
		exit 1
	fi

	if ! kill -0 "$PID" 2>/dev/null; then
		echo "gof5 is not running (stale PID file removed)"
		rm -f "$PID_FILE"
		exit 1
	fi

	echo "Stopping gof5 (PID: $PID)..."
	kill "$PID"

	# Wait for process to terminate
	for i in {1..10}; do
		if ! kill -0 "$PID" 2>/dev/null; then
			echo "gof5 stopped"
			rm -f "$PID_FILE"
			exit 0
		fi
		sleep 1
	done

	echo "Failed to stop gof5 gracefully, forcing..."
	kill -9 "$PID" 2>/dev/null
	rm -f "$PID_FILE"
}

status() {
	if [[ -f "$PID_FILE" ]]; then
		PID=$(cat "$PID_FILE" 2>/dev/null)
		if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
			echo "gof5 is running (PID: $PID)"
			exit 0
		else
			echo "gof5 is not running (stale PID file)"
			exit 1
		fi
	else
		echo "gof5 is not running"
		exit 1
	fi
}

restart() {
	stop 2>/dev/null || true
	sleep 2
	start
}

logs() {
	if [[ -f "$LOG_FILE" ]]; then
		tail -f "$LOG_FILE"
	else
		echo "No log file found at $LOG_FILE"
		exit 1
	fi
}

case "$1" in
start)
	start
	;;
stop)
	stop
	;;
restart)
	restart
	;;
status)
	status
	;;
logs)
	logs
	;;
*)
	echo "Usage: $0 {start|stop|restart|status|logs}"
	exit 1
	;;
esac
