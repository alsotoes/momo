import sys

def process(content):
    search = """				if c1 == -1 {
					return sigV4Components{}, false
				}
				c2 := strings.IndexByte(cred[c1+1:], '/')
				if c2 == -1 {
					return sigV4Components{}, false
				}
				c2 += c1 + 1
				c3 := strings.IndexByte(cred[c2+1:], '/')
				if c3 == -1 {
					return sigV4Components{}, false
				}
				c3 += c2 + 1
				c4 := strings.IndexByte(cred[c3+1:], '/')
				if c4 == -1 {
					return sigV4Components{}, false
				}"""
    replace = """				if c1 == -1 {
					return sigV4Components{}, syscall.EBADMSG
				}
				c2 := strings.IndexByte(cred[c1+1:], '/')
				if c2 == -1 {
					return sigV4Components{}, syscall.EBADMSG
				}
				c2 += c1 + 1
				c3 := strings.IndexByte(cred[c2+1:], '/')
				if c3 == -1 {
					return sigV4Components{}, syscall.EBADMSG
				}
				c3 += c2 + 1
				c4 := strings.IndexByte(cred[c3+1:], '/')
				if c4 == -1 {
					return sigV4Components{}, syscall.EBADMSG
				}"""
    content = content.replace(search, replace)
    return content

with open('src/transport/sigv4.go', 'r') as f:
    content = f.read()
content = process(content)
with open('src/transport/sigv4.go', 'w') as f:
    f.write(content)


def process_communicator(content):
    search = """		components, ok := parseSigV4AuthHeader(authHeader)
		if !ok {"""
    replace = """		components, err := parseSigV4AuthHeader(authHeader)
		if err != nil {"""
    content = content.replace(search, replace)

    search2 = """			if comps, ok := parseSigV4AuthHeader(authHeader); ok {"""
    replace2 = """			if comps, err := parseSigV4AuthHeader(authHeader); err == nil {"""
    content = content.replace(search2, replace2)
    return content

with open('src/transport/s3_communicator.go', 'r') as f:
    content = f.read()
content = process_communicator(content)
with open('src/transport/s3_communicator.go', 'w') as f:
    f.write(content)
