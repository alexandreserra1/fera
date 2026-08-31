package store

import (
	"testing"
	"time"

	"github.com/ale/fera/internal/sim"
)

const (
	setorBytes = 4096
	setores    = 7 // 2 pro estado (duplo buffer) + 1 pra credencial + 4 pra fila
)

var t0 = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)

func novoStore(t *testing.T) (*Store, *MemFlash) {
	t.Helper()
	f := NewMemFlash(setores, setorBytes)
	s, err := Open(f, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}

func evento(id string, min int) sim.Event {
	return sim.Event{
		ID: id, At: t0.Add(time.Duration(min) * time.Minute),
		Kind: sim.KindEffort, Kcal: 420, Zone: 3,
	}
}

func TestFlashVirgemNaoTemEstado(t *testing.T) {
	s, _ := novoStore(t)
	if _, err := s.LoadState(); err != ErrVazio {
		t.Errorf("erro = %v, esperado ErrVazio", err)
	}
	if p, err := s.Pending(); err != nil || len(p) != 0 {
		t.Errorf("fila virgem tem %d eventos (err=%v)", len(p), err)
	}
}

func TestEstadoSobreviveAoReboot(t *testing.T) {
	s, f := novoStore(t)

	want := sim.Fold(sim.Genesis("pet1", t0), []sim.Event{
		evento("01JA", 60), {ID: "01JB", At: t0.Add(2 * time.Hour), Kind: sim.KindEncounter, PeerID: "p2"},
	}, sim.DefaultTuning())

	if err := s.SaveState(want); err != nil {
		t.Fatal(err)
	}

	// reboot: Store novo em cima da MESMA flash
	s2, err := Open(f, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("estado não sobreviveu ao reboot\n got: %+v\nwant: %+v", got, want)
	}
}

// O duplo buffer existe pra isto: queda de energia no meio da escrita não pode
// levar junto o último estado bom. Grava alternando setor, e o mais novo só
// vale quando termina de escrever.
func TestQuedaDeEnergiaNoMeioDaEscritaPreservaOEstadoAnterior(t *testing.T) {
	s, f := novoStore(t)

	bom := sim.Fold(sim.Genesis("pet1", t0), []sim.Event{evento("01JA", 60)}, sim.DefaultTuning())
	if err := s.SaveState(bom); err != nil {
		t.Fatal(err)
	}

	novo := sim.Fold(bom, []sim.Event{evento("01JB", 120)}, sim.DefaultTuning())
	if err := s.SaveState(novo); err != nil {
		t.Fatal(err)
	}
	// simula escrita interrompida: o CRC do registro mais novo vira lixo
	if !s.CorromperUltimoEstado(f) {
		t.Fatal("a corrupção não mudou byte nenhum: o teste não testaria nada")
	}

	s2, err := Open(f, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.LoadState()
	if err != nil {
		t.Fatalf("os dois buffers foram perdidos: %v", err)
	}
	if got != bom {
		t.Errorf("caiu no estado errado\n got: %+v\nwant: %+v (o anterior, íntegro)", got, bom)
	}
}

// A regra do docs/06: escrever a cada tick mata o device em um ano. Salvar
// estado N vezes não pode custar N apagamentos no mesmo setor.
func TestSaveStateAlternaSetorParaEspalharDesgaste(t *testing.T) {
	s, f := novoStore(t)

	const n = 40
	for i := 0; i < n; i++ {
		if err := s.SaveState(sim.Genesis("pet1", t0.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}

	a, b := f.Apagamentos[0], f.Apagamentos[1]
	if a+b > n {
		t.Errorf("%d saves geraram %d apagamentos, esperado no máximo %d", n, a+b, n)
	}
	if a == 0 || b == 0 {
		t.Errorf("desgaste concentrado num setor só: setor0=%d setor1=%d", a, b)
	}
	if d := a - b; d > 1 || d < -1 {
		t.Errorf("desgaste desequilibrado: setor0=%d setor1=%d", a, b)
	}
}

func TestPendentesSobrevivemAoReboot(t *testing.T) {
	s, f := novoStore(t)

	want := []sim.Event{
		evento("01JA", 10),
		{ID: "01JB", At: t0.Add(20 * time.Minute), Kind: sim.KindSleep, Minutes: 430},
		{ID: "01JC", At: t0.Add(30 * time.Minute), Kind: sim.KindInteract},
		{ID: "01JD", At: t0.Add(40 * time.Minute), Kind: sim.KindEncounter, PeerID: "01JPEER"},
	}
	for _, ev := range want {
		if err := s.AppendPending(ev); err != nil {
			t.Fatal(err)
		}
	}

	s2, err := Open(f, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("voltaram %d de %d eventos", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("evento %d não sobreviveu\n got: %+v\nwant: %+v", i, got[i], want[i])
		}
	}
}

// A fila é append-only: enfileirar N eventos não pode apagar setor nenhum.
// Se apagasse, o device gastaria ciclo de flash a cada botão pressionado.
func TestAppendPendingNuncaApagaSetor(t *testing.T) {
	s, f := novoStore(t)

	for i := 0; i < 50; i++ {
		if err := s.AppendPending(evento(idSeq(i), i)); err != nil {
			t.Fatal(err)
		}
	}
	for setor, n := range f.Apagamentos {
		if n != 0 {
			t.Errorf("setor %d foi apagado %d vezes só enfileirando", setor, n)
		}
	}
}

// Marcar como sincronizado também é append-only: só zera bits do byte de
// status, sem reescrever o registro e sem apagar setor.
func TestMarkSyncedNaoApagaSetor(t *testing.T) {
	s, f := novoStore(t)
	for i := 0; i < 10; i++ {
		if err := s.AppendPending(evento(idSeq(i), i)); err != nil {
			t.Fatal(err)
		}
	}
	antes := somaApagamentos(f)

	if err := s.MarkSynced(10); err != nil {
		t.Fatal(err)
	}
	if depois := somaApagamentos(f); depois != antes {
		t.Errorf("MarkSynced apagou %d setores", depois-antes)
	}

	p, err := s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 0 {
		t.Errorf("%d eventos continuaram pendentes depois do sync", len(p))
	}
}

func TestMarkSyncedParcialDeixaOResto(t *testing.T) {
	s, _ := novoStore(t)
	for i := 0; i < 10; i++ {
		if err := s.AppendPending(evento(idSeq(i), i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MarkSynced(4); err != nil {
		t.Fatal(err)
	}

	p, err := s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 6 {
		t.Fatalf("sobraram %d pendentes, esperado 6", len(p))
	}
	if p[0].ID != idSeq(4) {
		t.Errorf("o primeiro pendente é %s, esperado %s", p[0].ID, idSeq(4))
	}
}

// Fila cheia é erro explícito, nunca descarte silencioso: evento perdido é
// treino que o dono fez e o bicho não comeu.
func TestFilaCheiaDaErroEmVezDeDescartar(t *testing.T) {
	s, _ := novoStore(t)

	n := 0
	for {
		err := s.AppendPending(evento(idSeq(n), n))
		if err == ErrFilaCheia {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
		if n > 10000 {
			t.Fatal("a fila nunca encheu")
		}
	}
	t.Logf("a fila coube %d eventos em %d setores de %d bytes", n, setores-primeiroFila, setorBytes)

	p, err := s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != n {
		t.Errorf("%d pendentes na fila, mas %d foram aceitos: sumiu evento", len(p), n)
	}
}

// Depois de sincronizar, o espaço volta. É aqui que o Erase acontece, e só
// aqui: um setor inteiro de registros já sincronizados.
func TestEspacoVoltaDepoisDoSync(t *testing.T) {
	s, f := novoStore(t)

	n := 0
	for s.AppendPending(evento(idSeq(n), n)) == nil {
		n++
	}
	if err := s.MarkSynced(n); err != nil {
		t.Fatal(err)
	}

	if err := s.AppendPending(evento("DEPOIS", 1)); err != nil {
		t.Fatalf("a fila continuou cheia mesmo com tudo sincronizado: %v", err)
	}
	if somaApagamentos(f) == 0 {
		t.Error("liberou espaço sem apagar setor nenhum: isso não é possível em NOR")
	}

	p, err := s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 1 || p[0].ID != "DEPOIS" {
		t.Errorf("a fila tem %+v, esperado só o evento novo", p)
	}
}

// Registro corrompido não pode derrubar o device nem contaminar a fila.
func TestRegistroCorrompidoEhIgnoradoSemDerrubar(t *testing.T) {
	s, f := novoStore(t)
	for i := 0; i < 5; i++ {
		if err := s.AppendPending(evento(idSeq(i), i)); err != nil {
			t.Fatal(err)
		}
	}
	if !s.CorromperPendente(f, 2) {
		t.Fatal("a corrupção não mudou byte nenhum: o teste não testaria nada")
	}

	s2, err := Open(f, setorBytes, setores)
	if err != nil {
		t.Fatalf("Open derrubou por causa de um registro corrompido: %v", err)
	}
	p, err := s2.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 4 {
		t.Errorf("%d pendentes, esperado 4 (um corrompido descartado)", len(p))
	}
	for _, ev := range p {
		if ev.ID == idSeq(2) {
			t.Error("o registro corrompido entrou na fila")
		}
	}
}

func idSeq(i int) string {
	const base = "01J00000000000000000000000"
	b := []byte(base)
	b[len(b)-1] = byte('A' + i%26)
	b[len(b)-2] = byte('A' + (i/26)%26)
	return string(b)
}

func somaApagamentos(f *MemFlash) int {
	n := 0
	for _, v := range f.Apagamentos {
		n += v
	}
	return n
}

// A conta que o docs/06 faz em prosa, virada teste.
//
// "NVS tem ordem de 100 mil ciclos de escrita por setor. Se você salvar a cada
// tick de 5 min, são 105 mil escritas por ano e o device morre."
//
// Este teste simula um ano dos dois jeitos e mostra a diferença em anos de
// vida. Se alguém passar a salvar estado no laço, o número despenca aqui antes
// de despencar numa placa.
func TestVidaDaFlashEmUsoRealista(t *testing.T) {
	const (
		ciclosPorSetor = 100_000
		dias           = 365
	)

	// Uso realista, seguindo a regra do docs/06: salva em mudança de estágio,
	// botão pressionado, antes de deep sleep por bateria, e depois de sync.
	medir := func(savesPorDia int) (apagMax int, anos float64) {
		s, f := novoStore(t)
		for d := 0; d < dias; d++ {
			for i := 0; i < savesPorDia; i++ {
				if err := s.SaveState(sim.Genesis("pet1", t0)); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, n := range f.Apagamentos[:setoresEstado] {
			if n > apagMax {
				apagMax = n
			}
		}
		return apagMax, float64(ciclosPorSetor) / float64(apagMax)
	}

	realista, anosRealista := medir(7) // ~5 botões + 2 syncs por dia
	t.Logf("uso realista (7 saves/dia): %d apagamentos por setor ao ano -> %.0f anos", realista, anosRealista)
	if anosRealista < 20 {
		t.Errorf("o device dura só %.0f anos em uso realista", anosRealista)
	}

	// O anti-padrão que o docs/06 alerta: salvar a cada tick de 5 minutos.
	tick, anosTick := medir(288)
	t.Logf("salvando a cada tick de 5min: %d apagamentos por setor ao ano -> %.1f anos", tick, anosTick)
	if anosTick > 5 {
		t.Errorf("o modelo do anti-padrão não reproduz o problema do docs/06: deu %.1f anos", anosTick)
	}
}

// A fila é o caminho quente: o dono aperta botão e um evento nasce. Se
// enfileirar custasse apagamento, o botão seria a coisa que mata o device.
func TestVidaDaFilaEmUsoRealista(t *testing.T) {
	s, f := novoStore(t)

	// um ano de 5 eventos por dia, sincronizando a cada 12 horas
	const eventosPorDia = 5
	for d := 0; d < 365; d++ {
		for i := 0; i < eventosPorDia; i++ {
			if err := s.AppendPending(evento(idSeq(i), i)); err != nil {
				t.Fatalf("dia %d: %v", d, err)
			}
		}
		p, err := s.Pending()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.MarkSynced(len(p)); err != nil {
			t.Fatal(err)
		}
	}

	pior := 0
	for _, n := range f.Apagamentos[primeiroFila:] {
		if n > pior {
			pior = n
		}
	}
	anos := 100_000.0 / float64(pior)
	t.Logf("fila: %d apagamentos por setor ao ano -> %.0f anos", pior, anos)
	if anos < 100 {
		t.Errorf("a fila gasta a flash rápido demais: %.0f anos", anos)
	}
}

// A regra de alocação zero, igual à do renderer. AppendPending roda a cada
// botão pressionado; alocar ali acorda o GC num device que deveria dormir.
func TestAppendPendingNaoAloca(t *testing.T) {
	s, _ := novoStore(t)
	ev := evento("01JA", 1)
	i := 0

	n := testing.AllocsPerRun(100, func() {
		// a fila enche e reseta pra não medir o custo do Erase
		if i%150 == 0 {
			p, _ := s.Pending()
			_ = s.MarkSynced(len(p))
		}
		i++
		_ = s.AppendPending(ev)
	})
	if n != 0 {
		t.Errorf("AppendPending alocou %v vezes, esperado 0", n)
	}
}

// Registro que atravessa fronteira de setor é corrupção esperando acontecer:
// apagar um setor destrói a metade do registro que mora no vizinho. Com
// registro de 80 bytes num setor de 4096 isso acontece a partir do slot 51.
func TestRegistroNuncaAtravessaFronteiraDeSetor(t *testing.T) {
	s, _ := novoStore(t)

	for i := int64(0); i < s.slots; i++ {
		ini := s.offSlot(i)
		fim := ini + tamRegFila - 1
		if ini/setorBytes != fim/setorBytes {
			t.Fatalf("slot %d ocupa os bytes %d..%d e cruza do setor %d pro %d",
				i, ini, fim, ini/setorBytes, fim/setorBytes)
		}
		if s.setorDoSlot(i) != ini/setorBytes {
			t.Errorf("slot %d: setorDoSlot diz %d, mas o offset cai no setor %d",
				i, s.setorDoSlot(i), ini/setorBytes)
		}
	}
}

// Dar a volta na fila muitas vezes não pode perder nem inventar evento.
func TestFilaDaVoltaVariasVezesSemPerderEvento(t *testing.T) {
	s, _ := novoStore(t)

	for volta := 0; volta < 5; volta++ {
		for i := int64(0); i < s.slots; i++ {
			if err := s.AppendPending(evento(idSeq(int(i%676)), int(i))); err != nil {
				t.Fatalf("volta %d, slot %d: %v", volta, i, err)
			}
		}
		p, err := s.Pending()
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(p)) != s.slots {
			t.Fatalf("volta %d: %d pendentes, esperado %d", volta, len(p), s.slots)
		}
		if err := s.MarkSynced(len(p)); err != nil {
			t.Fatal(err)
		}
	}
}

// Reboot DEPOIS que a fila deu a volta é o caso que a busca ingênua erra.
//
// Nesse estado existem slots livres ANTES dos gravados, então "primeiro slot
// não-livre a partir do zero" põe a cabeça no meio do histórico: o device
// acha que a fila está cheia, ou pior, reusa seq e embaralha a ordem de
// envio. A cabeça tem que sair do maior seq, não da varredura.
func TestRebootDepoisDaVoltaAchaACabecaCerta(t *testing.T) {
	s, f := novoStore(t)

	// enche, sincroniza, e dá a volta com alguns eventos novos
	n := 0
	for s.AppendPending(evento(idSeq(n), n)) == nil {
		n++
	}
	if err := s.MarkSynced(n); err != nil {
		t.Fatal(err)
	}

	const depoisDaVolta = 10
	for i := 0; i < depoisDaVolta; i++ {
		if err := s.AppendPending(evento("VOLTA"+string(rune('A'+i)), i)); err != nil {
			t.Fatalf("evento %d depois da volta: %v", i, err)
		}
	}

	// reboot
	s2, err := Open(f, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}

	p, err := s2.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != depoisDaVolta {
		t.Fatalf("depois do reboot há %d pendentes, esperado %d", len(p), depoisDaVolta)
	}
	for i, ev := range p {
		if want := "VOLTA" + string(rune('A'+i)); ev.ID != want {
			t.Errorf("pendente %d é %s, esperado %s: a ordem de envio embaralhou", i, ev.ID, want)
		}
	}

	// e continuar enfileirando não pode sobrescrever o que está pendente
	if err := s2.AppendPending(evento("DEPOISDOREBOOT", 1)); err != nil {
		t.Fatalf("enfileirar depois do reboot falhou: %v", err)
	}
	p2, err := s2.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(p2) != depoisDaVolta+1 {
		t.Errorf("%d pendentes depois de enfileirar, esperado %d: sobrescreveu",
			len(p2), depoisDaVolta+1)
	}
	if p2[len(p2)-1].ID != "DEPOISDOREBOOT" {
		t.Errorf("o último pendente é %s, esperado DEPOISDOREBOOT", p2[len(p2)-1].ID)
	}
}

// O token aparece uma vez só, no register. Se não sobreviver ao reboot, o
// device perde o acesso e vira um bicho que nunca mais sincroniza.
func TestCredenciaisSobrevivemAoReboot(t *testing.T) {
	s, f := novoStore(t)

	if _, err := s.LoadCreds(); err != ErrVazio {
		t.Errorf("flash virgem devolveu credencial: %v", err)
	}

	want := Creds{
		BaseURL:  "https://fera.exemplo.br",
		PetID:    "2ce9dda3-ead1-4ba8-925d-d0d6633a6d1b",
		DeviceID: "8f1c0b52-9a3d-4e77-b1aa-6c5e2d9f0431",
		Token:    "qQi4sCpgILvVnHqZ8dKm3xPfTgRwYbNcJhLoAeUiSvE",
	}
	if err := s.SaveCreds(want); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(f, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.LoadCreds()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("credenciais não sobreviveram\n got: %+v\nwant: %+v", got, want)
	}
}

// Credencial é escrita uma vez na vida do device. Ela não pode dividir setor
// com estado nem com a fila: um apagamento de qualquer um dos dois levaria o
// token junto.
func TestCredenciaisNaoCompartilhamSetorComEstadoNemFila(t *testing.T) {
	s, f := novoStore(t)

	if err := s.SaveCreds(Creds{BaseURL: "u", PetID: "p", DeviceID: "d", Token: "t"}); err != nil {
		t.Fatal(err)
	}

	// muita escrita de estado e de fila não pode encostar na credencial
	for i := range 30 {
		if err := s.SaveState(sim.Genesis("pet1", t0.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for s.AppendPending(evento(idSeq(n), n)) == nil {
		n++
	}
	if err := s.MarkSynced(n); err != nil {
		t.Fatal(err)
	}
	for i := range 50 {
		if err := s.AppendPending(evento(idSeq(i), i)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.LoadCreds()
	if err != nil {
		t.Fatalf("a credencial sumiu depois de uso normal: %v", err)
	}
	if got.Token != "t" {
		t.Errorf("token = %q, esperado %q", got.Token, "t")
	}
	if f.Apagamentos[setorCreds] != 1 {
		t.Errorf("o setor de credencial foi apagado %d vezes, esperado 1",
			f.Apagamentos[setorCreds])
	}
}

// Credencial corrompida não pode virar token lixo enviado ao servidor: melhor
// não ter credencial e registrar de novo.
func TestCredencialCorrompidaEhTratadaComoAusente(t *testing.T) {
	s, f := novoStore(t)
	if err := s.SaveCreds(Creds{BaseURL: "u", PetID: "p", DeviceID: "d", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if !f.Corromper(int64(setorCreds)*setorBytes+offCredsCRC+1, 0x00) {
		t.Fatal("a corrupção não mudou byte nenhum")
	}

	s2, err := Open(f, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.LoadCreds(); err != ErrVazio {
		t.Errorf("erro = %v, esperado ErrVazio", err)
	}
}
