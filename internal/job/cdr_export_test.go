package job

import (
	"testing"

	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

func TestCDRExportProcessorReadPoolSeparation(t *testing.T) {
	primaryCDRStore := store.NewCDRStore(nil)
	readCDRStore := store.NewCDRStore(nil)

	proc := NewCDRExportProcessor(
		store.NewJobStore(nil),
		primaryCDRStore,
		readCDRStore,
		nil,
		zerolog.Nop(),
	)

	if proc.cdrStore != primaryCDRStore {
		t.Error("primary cdrStore must point to primary pool store")
	}
	if proc.readCDRStore != readCDRStore {
		t.Error("readCDRStore must point to read pool store")
	}
	if proc.cdrStore == proc.readCDRStore {
		t.Error("cdrStore and readCDRStore must be separate instances when different pools are used")
	}
}

func TestCDRExportProcessorPrimaryUsedWhenNoReadReplica(t *testing.T) {
	primaryCDRStore := store.NewCDRStore(nil)

	proc := NewCDRExportProcessor(
		store.NewJobStore(nil),
		primaryCDRStore,
		primaryCDRStore,
		nil,
		zerolog.Nop(),
	)

	if proc.cdrStore != primaryCDRStore {
		t.Error("cdrStore must be primary CDR store")
	}
	if proc.readCDRStore != primaryCDRStore {
		t.Error("readCDRStore must fall back to primary CDR store when no replica")
	}
	if proc.cdrStore != proc.readCDRStore {
		t.Error("cdrStore and readCDRStore must be the same instance when no read replica")
	}
}

func TestCDRExportProcessorType(t *testing.T) {
	cdrStore := store.NewCDRStore(nil)

	proc := NewCDRExportProcessor(
		store.NewJobStore(nil),
		cdrStore,
		cdrStore,
		nil,
		zerolog.Nop(),
	)

	if proc.Type() != JobTypeCDRExport {
		t.Errorf("Type() = %q, want %q", proc.Type(), JobTypeCDRExport)
	}
}
