import re

with open("src/transport/s3_versioning.go", "r") as f:
    content = f.read()


search1 = """	// ⚡ Bolt: Eliminate dynamic string allocations and repeated formatting overhead
	// by replacing xml.MarshalIndent with a bytes.Buffer and using AppendFormat.
	var timeBuf [32]byte
	for _, rec := range page.Records {
		if rec.IsDeleteMarker {
			buf.WriteString("<DeleteMarker>")
		} else {
			buf.WriteString("<Version>")
		}

		buf.WriteString("<Key>")
		xmlEscape(&buf, rec.Key)
		buf.WriteString("</Key>")

		buf.WriteString("<VersionId>")
		xmlEscape(&buf, rec.VersionID)
		buf.WriteString("</VersionId>")

		buf.WriteString("<IsLatest>")
		if rec.IsLatest {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		buf.WriteString("</IsLatest>")

		buf.WriteString("<LastModified>")
		t := time.Unix(0, rec.Timestamp).UTC()
		buf.Write(t.AppendFormat(timeBuf[:0], time.RFC3339Nano))
		buf.WriteString("</LastModified>")

		if !rec.IsDeleteMarker {
			buf.WriteString("<ETag>\"")
			xmlEscape(&buf, rec.Hash)
			buf.WriteString("\"</ETag>")

			buf.WriteString("<Size>")
			buf.Write(strconv.AppendInt(intBuf[:0], int64(rec.Size), 10))
			buf.WriteString("</Size>")

			buf.WriteString("<StorageClass>STANDARD</StorageClass>")
		}

		if rec.IsDeleteMarker {
			buf.WriteString("</DeleteMarker>")
		} else {
			buf.WriteString("</Version>")
		}
	}
	buf.WriteString("</ListVersionsResult>\\n")
	return buf.Bytes(), nil
}"""

replace1 = """	// ⚡ Bolt: Eliminate dynamic string allocations and repeated formatting overhead
	// by replacing xml.MarshalIndent with a bytes.Buffer and using AppendFormat.
	var timeBuf [32]byte
	for _, rec := range page.Records {
		formatVersionRecord(&buf, rec, &timeBuf, &intBuf)
	}
	buf.WriteString("</ListVersionsResult>\\n")
	return buf.Bytes(), nil
}

// formatVersionRecord writes a single version or delete marker entry directly to the buffer.
// Extracted to lower cognitive complexity.
func formatVersionRecord(buf *bytes.Buffer, rec storage.VersionRecord, timeBuf *[32]byte, intBuf *[32]byte) {
	if rec.IsDeleteMarker {
		buf.WriteString("<DeleteMarker>")
	} else {
		buf.WriteString("<Version>")
	}

	buf.WriteString("<Key>")
	xmlEscape(buf, rec.Key)
	buf.WriteString("</Key>")

	buf.WriteString("<VersionId>")
	xmlEscape(buf, rec.VersionID)
	buf.WriteString("</VersionId>")

	buf.WriteString("<IsLatest>")
	if rec.IsLatest {
		buf.WriteString("true")
	} else {
		buf.WriteString("false")
	}
	buf.WriteString("</IsLatest>")

	buf.WriteString("<LastModified>")
	t := time.Unix(0, rec.Timestamp).UTC()
	buf.Write(t.AppendFormat(timeBuf[:0], time.RFC3339Nano))
	buf.WriteString("</LastModified>")

	if !rec.IsDeleteMarker {
		buf.WriteString("<ETag>\"")
		xmlEscape(buf, rec.Hash)
		buf.WriteString("\"</ETag>")

		buf.WriteString("<Size>")
		buf.Write(strconv.AppendInt(intBuf[:0], int64(rec.Size), 10))
		buf.WriteString("</Size>")

		buf.WriteString("<StorageClass>STANDARD</StorageClass>")
	}

	if rec.IsDeleteMarker {
		buf.WriteString("</DeleteMarker>")
	} else {
		buf.WriteString("</Version>")
	}
}"""

content = content.replace(search1, replace1)


search2 = """func (m *S3Communicator) handleVersionedDelete(bucket, key, versionID string, now int64, bypass bool) (int, int64, error) {
	if versionID == "" {
		// Delete marker: shadow the object by removing the flat mapping so
		// plain GETs 404, then record the marker version.
		if err := m.versioning.RepointNamespace(key, ""); err != nil {
			writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to delete object.", key)
			return 0, 0, fmt.Errorf("failed to remove mapping for delete marker: %w", err)
		}
		marker, err := m.versioning.RecordDeleteMarker(bucket, key)
		if err != nil {
			writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to create delete marker.", key)
			return 0, 0, fmt.Errorf("failed to record delete marker: %w", err)
		}
		hdr := "HTTP/1.1 204 No Content\\r\\nx-amz-delete-marker: true\\r\\nx-amz-version-id: " + marker + s3ConnClose
		m.writeAuditLog("DeleteObject", bucket+"/"+key, "success")
		return 0, 0, m.writeRawResponse(hdr, nil, nil)
	}

	// Permanent delete of one specific version.
	deleted, err := m.versioning.DeleteVersion(bucket, key, versionID, now, bypass)
	if err != nil {
		if err == syscall.ENOENT {
			writeS3Error(m.conn, http.StatusNotFound, "NoSuchVersion", "The specified version does not exist.", key)
			return 0, 0, ErrRequestHandled
		}
		if err == syscall.EACCES {
			// R10 Phase 4 (#938): the version is protected by retention or a legal hold.
			_, _ = writeS3Error(m.conn, http.StatusForbidden, "AccessDenied", "The specified version is protected by an object lock.", key)
			return 0, 0, ErrRequestHandled
		}
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to delete the specified version.", key)
		return 0, 0, fmt.Errorf("failed to delete version: %w", err)
	}

	// Keep the flat namespace aligned with the surviving latest record.
	if latest, ok, lerr := m.versioning.LatestVersion(bucket, key); lerr == nil && ok {
		if latest.IsDeleteMarker {
			_ = m.versioning.RepointNamespace(key, "")
		} else {
			_ = m.versioning.RepointNamespace(key, latest.Hash)
		}
	}
	marker := "false"
	if deleted.IsDeleteMarker {
		marker = "true"
	}
	hdr := "HTTP/1.1 204 No Content\\r\\nx-amz-delete-marker: " + marker + "\\r\\nx-amz-version-id: " + deleted.VersionID + s3ConnClose
	m.writeAuditLog("DeleteObjectVersion", bucket+"/"+key, "success")
	return 0, 0, m.writeRawResponse(hdr, nil, nil)
}"""

replace2 = """func (m *S3Communicator) handleVersionedDelete(bucket, key, versionID string, now int64, bypass bool) (int, int64, error) {
	if versionID == "" {
		return m.handleDeleteMarkerCreation(bucket, key)
	}
	return m.handlePermanentVersionDelete(bucket, key, versionID, now, bypass)
}

func (m *S3Communicator) handleDeleteMarkerCreation(bucket, key string) (int, int64, error) {
	// Delete marker: shadow the object by removing the flat mapping so
	// plain GETs 404, then record the marker version.
	if err := m.versioning.RepointNamespace(key, ""); err != nil {
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to delete object.", key)
		return 0, 0, fmt.Errorf("failed to remove mapping for delete marker: %w", err)
	}
	marker, err := m.versioning.RecordDeleteMarker(bucket, key)
	if err != nil {
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to create delete marker.", key)
		return 0, 0, fmt.Errorf("failed to record delete marker: %w", err)
	}
	hdr := "HTTP/1.1 204 No Content\\r\\nx-amz-delete-marker: true\\r\\nx-amz-version-id: " + marker + s3ConnClose
	m.writeAuditLog("DeleteObject", bucket+"/"+key, "success")
	return 0, 0, m.writeRawResponse(hdr, nil, nil)
}

func (m *S3Communicator) handlePermanentVersionDelete(bucket, key, versionID string, now int64, bypass bool) (int, int64, error) {
	// Permanent delete of one specific version.
	deleted, err := m.versioning.DeleteVersion(bucket, key, versionID, now, bypass)
	if err != nil {
		if err == syscall.ENOENT {
			writeS3Error(m.conn, http.StatusNotFound, "NoSuchVersion", "The specified version does not exist.", key)
			return 0, 0, ErrRequestHandled
		}
		if err == syscall.EACCES {
			// R10 Phase 4 (#938): the version is protected by retention or a legal hold.
			_, _ = writeS3Error(m.conn, http.StatusForbidden, "AccessDenied", "The specified version is protected by an object lock.", key)
			return 0, 0, ErrRequestHandled
		}
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to delete the specified version.", key)
		return 0, 0, fmt.Errorf("failed to delete version: %w", err)
	}

	// Keep the flat namespace aligned with the surviving latest record.
	if latest, ok, lerr := m.versioning.LatestVersion(bucket, key); lerr == nil && ok {
		if latest.IsDeleteMarker {
			_ = m.versioning.RepointNamespace(key, "")
		} else {
			_ = m.versioning.RepointNamespace(key, latest.Hash)
		}
	}
	marker := "false"
	if deleted.IsDeleteMarker {
		marker = "true"
	}
	hdr := "HTTP/1.1 204 No Content\\r\\nx-amz-delete-marker: " + marker + "\\r\\nx-amz-version-id: " + deleted.VersionID + s3ConnClose
	m.writeAuditLog("DeleteObjectVersion", bucket+"/"+key, "success")
	return 0, 0, m.writeRawResponse(hdr, nil, nil)
}"""

content = content.replace(search2, replace2)

with open("src/transport/s3_versioning.go", "w") as f:
    f.write(content)
