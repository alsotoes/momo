No blog post: this is a security hardening of an existing surface, not a new
engineering decision. It adds a bearer gate to the admin listener and refuses the
mutating endpoint on a non-loopback bind when no token is configured; the
architecture and rationale are captured in ARCHITECTURE §7b and the OpenSpec
change. The narrative posts closest to it are 067 (coordination election) and 022
(secrets), which remain accurate.
