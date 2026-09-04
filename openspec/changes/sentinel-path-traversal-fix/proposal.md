# Sentinel: Fix Path Traversal Bypass via Sanitization

Issue: https://github.com/alsotoes/momo/issues/450
Status: Accepted
Context: Path traversal checks were being performed on network buffers after they had been passed through common.SanitizeLog(). Sanitization functions alter or remove control characters (including null bytes) and potentially path traversal sequences. If the raw input contained path traversal sequences, SanitizeLog might alter them, allowing the subsequent path traversal check to pass and admitting a malicious (but altered) payload.
