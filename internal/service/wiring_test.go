package service

import (
	"testing"

	"github.com/ale/fera/internal/repo"
)

// As interfaces daqui são pequenas e não exportadas, então nada garante que os
// repos de verdade as satisfaçam até o main tentar montar tudo. Esta asserção
// antecipa esse erro pro `go test`, em vez de deixar a fatia seguinte
// descobrir na hora do wiring.
//
// É a única razão pra este pacote enxergar o repo, e só em arquivo _test.
func TestReposSatisfazemAsInterfaces(t *testing.T) {
	var (
		_ eventStore    = (*repo.EventRepo)(nil)
		_ snapshotStore = (*repo.SnapshotRepo)(nil)
	)
}
