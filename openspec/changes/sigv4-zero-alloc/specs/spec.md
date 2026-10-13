# Technical Specification

- Replace `strings.Split` with `strings.IndexByte`.
- Use a stack-allocated buffer for common header sizes (`var stackHeaders [16]string`).
- Bypass `net/textproto` allocations by doing manual iteration over `req.Header` checking `strings.EqualFold`.
- Enforce a 4096-byte length limit on parsed header values (Rule 35).
- Wrap the function in a `defer recover()` block mapping panics to `syscall.EIO` (Rule 37).
