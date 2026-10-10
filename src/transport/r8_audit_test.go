package transport

import "testing"

func TestS3Communicator_TenantID(t *testing.T) {
	m := &S3Communicator{tenantID: "tenant-a"}
	if m.TenantID() != "tenant-a" {
		t.Fatalf("TenantID = %q", m.TenantID())
	}
}

func TestMomoTCP_WriteAuditLog(t *testing.T) {
	ms := &mockStore{}
	m := &MomoTCPCommunicator{store: ms}
	m.writeAuditLog("GetObject", "file.txt", "success")
	if len(ms.auditEntries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(ms.auditEntries))
	}
	if e := ms.auditEntries[0]; e.Identity != "momo-tcp" || e.Operation != "GetObject" || e.Resource != "file.txt" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	// Nil store must be a no-op (no panic).
	(&MomoTCPCommunicator{}).writeAuditLog("GetObject", "file.txt", "success")
}

func TestMomoQUIC_WriteAuditLog(t *testing.T) {
	ms := &mockStore{}
	m := &MomoQUICCommunicator{store: ms}
	m.writeAuditLog("DeleteObject", "file.txt", "success")
	if len(ms.auditEntries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(ms.auditEntries))
	}
	if e := ms.auditEntries[0]; e.Identity != "momo-quic" || e.Operation != "DeleteObject" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	(&MomoQUICCommunicator{}).writeAuditLog("DeleteObject", "file.txt", "success")
}
