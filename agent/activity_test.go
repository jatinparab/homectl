package agent

import "testing"

func TestParseTCPTransferredBytes(t *testing.T) {
	output := `Recv-Q Send-Q Local Address:Port Peer Address:Port
0 0 172.20.0.2:54548 172.20.0.28:8096
	bytes_sent:3875 bytes_acked:3876 bytes_received:7046
0 0 172.20.0.2:54549 172.20.0.28:8096
	bytes_sent:100 bytes_received:200
`
	if got, want := parseTCPTransferredBytes(output), uint64(11221); got != want {
		t.Fatalf("transferred bytes = %d, want %d", got, want)
	}
}

func TestParseTCPTransferredBytesWithoutConnections(t *testing.T) {
	if got := parseTCPTransferredBytes("Recv-Q Send-Q Local Address:Port Peer Address:Port\n"); got != 0 {
		t.Fatalf("transferred bytes = %d, want 0", got)
	}
}
