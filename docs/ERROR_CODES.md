# Momo Error Codes and Exit Status

This document provides a reference for the error codes and exit statuses that the Momo application may produce. Understanding these codes is crucial for debugging and operating a Momo cluster effectively.

## Overview

Momo relies on standard POSIX system calls for network and file I/O. As such, many of the errors it encounters are standard Unix error codes. The application will typically print an error message to `stderr` that includes the specific error code and then exit with a non-zero status.

The general exit code for many fatal errors is `1`.

## Common System Call Errors

Below is a list of common `errno` values that might be encountered and their specific meaning in the context of Momo.

| Error Code | Constant | POSIX Meaning | Momo Context & Interpretation |
| :--- | :--- | :--- | :--- |
| `EPROTO` | Protocol error | A protocol error was detected on the communication link. | This is a critical error. It most likely means there is a bug or a version mismatch between the client and server, or that a non-Momo client is trying to connect. The handshake or message framing has failed. |
| `ENOLINK` | Link has been severed | A communication link has been severed. | This error is particularly relevant for **Chain Replication**. It indicates that a server in the middle of the chain has disconnected, breaking the replication flow. The file transfer will fail. |
| `EBADMSG` | Bad message | The message received does not conform to the protocol. | This often points to data corruption during transit or an issue with how the message was framed (e.g., incorrect metadata size). The MD5 checksum validation might also trigger conditions that lead to this. |
| `ETIMEDOUT`| Connection timed out | A connection attempt timed out, or a connected partner has not responded. | Could occur during the initial handshake if the server is unresponsive, or during the file transfer if there is a network partition or a server becomes overloaded and cannot respond. |
| `ECONNREFUSED`| Connection refused | The target machine actively refused the connection. | This is a straightforward network error. It means a Momo server is not running or is not reachable at the specified IP address and port, or a firewall is blocking the connection. |
| `EPIPE` | Broken pipe | An attempt was made to write to a pipe or socket that is not open for reading on the other end. | This commonly occurs if the client or a downstream server closes the connection while an upstream server is still trying to send data. |
| `ENOSPC` | No space left on device | An attempt to write a file to a device has failed because the device is full. | A server in the cluster has run out of disk space. The replication for the current file will fail on that specific server. |
| `EIO` | I/O error | A physical I/O error has occurred. | This indicates a problem with the underlying storage hardware on one of the servers. It is a serious error that points to potential disk failure. |

## Application-Specific Exit Codes

Momo may also use specific exit codes to signify particular failure modes.

| Exit Code | Meaning |
| :--- | :--- |
| `1` | **General Fatal Error:** This is the most common exit code for unrecoverable errors, such as the system call failures listed above. Check `stderr` for a more specific message. |
| `2` | **Configuration Error:** The `momo.conf` file is missing, malformed, or contains invalid values. The application cannot start without valid configuration. |
| `3` | **Permissions Error:** The application does not have the necessary permissions to read its configuration, write to the specified storage directory, or bind to the required network port. |

By cross-referencing the exit code with the error messages printed to standard error, operators can quickly diagnose and resolve issues within the Momo cluster.
