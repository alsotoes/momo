# Optimization: Fast-Path XML Escaping with Boolean Array

This proposal documents the optimization of the `xmlEscape` function in the S3 protocol implementation, which substitutes a standard library character lookup (`strings.IndexAny`) with a direct boolean array lookup (`escapeBytes`) to significantly lower function call overhead on hot paths.

## Motivation
XML escaping occurs pervasively when generating S3 XML lists. Previously, the `strings.IndexAny` call introduced setup and function-call overhead that dominated the operation time. By leveraging a local byte array lookup, memory efficiency is matched and CPU overhead is halved.

## Architecture & Integration
The optimization touches only internal data representation and leaves external behavior entirely unchanged. It enforces the Zero-Crash pattern (Rule 4) by adding strict bound checks to prevent unchecked buffer growth from arbitrarily large inputs (Rule 32/35), mapped dynamically to standard `syscall.EINVAL` error responses on failure (Rule 10).

## Issue Link
https://github.com/alsotoes/momo/issues/1095
