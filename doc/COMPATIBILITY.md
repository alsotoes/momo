# Momo Platform Compatibility

This document outlines the supported operating systems for building and running the Momo application. It also provides information on dependencies and platform-specific considerations.

## Platform Tiers

Momo's support for various operating systems is categorized into the following tiers:

### Tier 1: Officially Supported

These are the platforms where Momo is actively developed, tested, and expected to perform reliably.

-   **Linux:** (Kernel 4.x and newer) - This is the primary development and production environment for Momo.
-   **FreeBSD:** (12.x and newer) - Fully supported and regularly tested.

### Tier 2: Best-Effort Support

These platforms are expected to work, and may even have specific optimizations, but are not part of the regular, continuous testing cycle.

-   **macOS (Apple Silicon):** The inclusion of specific libraries for the M1 CPU architecture (`go-m1cpu`) indicates that Momo is aware of and should perform well on modern Apple hardware.
-   **DragonflyBSD:** The codebase contains specific system call definitions for DragonflyBSD, so it is expected to compile and run correctly. However, it is not a primary test platform.

### Tier 3: Experimental / Limited Support

These platforms are not officially supported. While Momo might compile or run, functionality is likely to be limited or unstable.

-   **Windows:** Momo's core design relies heavily on POSIX system calls and a Unix-like environment. While some of its dependencies have Windows compatibility (e.g., `go-ole`), the main application is **not expected to run natively on Windows**. Users seeking to run Momo on a Windows machine should use the **Windows Subsystem for Linux (WSL) 2**.
-   **Other Unix-like Systems:** Other POSIX-compliant systems (e.g., OpenBSD, NetBSD) may be able to build and run Momo, but they have not been tested.

## Build Dependencies

-   **Go Compiler:** A recent version of the Go compiler (1.18 or newer) is required.
-   **Standard C Compiler:** A C compiler like GCC or Clang is needed for certain dependencies that use cgo.

## Known Issues and Considerations

-   **Filesystem Performance:** The performance of file I/O can vary significantly between different operating systems and underlying filesystems.
-   **Networking Stack:** The behavior of the networking stack, particularly regarding TCP congestion control and error handling, can differ between kernels. The polymorphic system in Momo is designed to adapt to these variations, but extreme network conditions may expose platform-specific behavior.
-   **Windows (WSL):** When running under WSL 2, network performance and file I/O may not match bare-metal Linux performance. It is recommended to store the data directories within the Linux filesystem (`/`) rather than accessing them across the Windows mount point (`/mnt/c`).
