import sys

def process(content):
    # Only replace 'return sigV4Components{}, syscall.EBADMSG' in parseSigV4QueryAuth with 'return sigV4Components{}, false'

    search = """func parseSigV4QueryAuth(req *http.Request) (sigV4Components, bool) {
	q := req.URL.Query()
	if q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
		return sigV4Components{}, false
	}
	cred := q.Get("X-Amz-Credential")

	c1 := strings.IndexByte(cred, '/')
	if c1 == -1 {
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
	}

	c := sigV4Components{
		AccessKey:     cred[:c1],
		DateStamp:     cred[c1+1 : c2],
		Region:        cred[c2+1 : c3],
		SignedHeaders: q.Get("X-Amz-SignedHeaders"),
		Signature:     q.Get("X-Amz-Signature"),
		AmzDate:       q.Get("X-Amz-Date"),
	}
	if c.AccessKey == "" || c.Signature == "" || c.SignedHeaders == "" || c.AmzDate == "" {
		return sigV4Components{}, syscall.EBADMSG
	}
	return c, true
}"""
    replace = """func parseSigV4QueryAuth(req *http.Request) (sigV4Components, bool) {
	q := req.URL.Query()
	if q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
		return sigV4Components{}, false
	}
	cred := q.Get("X-Amz-Credential")

	c1 := strings.IndexByte(cred, '/')
	if c1 == -1 {
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
	}

	c := sigV4Components{
		AccessKey:     cred[:c1],
		DateStamp:     cred[c1+1 : c2],
		Region:        cred[c2+1 : c3],
		SignedHeaders: q.Get("X-Amz-SignedHeaders"),
		Signature:     q.Get("X-Amz-Signature"),
		AmzDate:       q.Get("X-Amz-Date"),
	}
	if c.AccessKey == "" || c.Signature == "" || c.SignedHeaders == "" || c.AmzDate == "" {
		return sigV4Components{}, false
	}
	return c, true
}"""
    if search in content:
        content = content.replace(search, replace)
    else:
        print("COULD NOT FIND SEARCH STRING")
    return content

with open('src/transport/sigv4.go', 'r') as f:
    content = f.read()

content = process(content)

with open('src/transport/sigv4.go', 'w') as f:
    f.write(content)
