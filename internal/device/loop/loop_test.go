package loop

import (
	"testing"
	"time"

	"github.com/ale/fera/internal/device/display"
	"github.com/ale/fera/internal/device/hal"
	"github.com/ale/fera/internal/device/store"
	"github.com/ale/fera/internal/device/ui"
	"github.com/ale/fera/internal/sim"
)

var t0 = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)

const (
	setorBytes = 4096
	setores    = 6
)

type sincFalso struct {
	chamadas int
	enviados int
	erro     error
}

func (s *sincFalso) Sync(pend []sim.Event) (int, error) {
	s.chamadas++
	if s.erro != nil {
		return 0, s.erro
	}
	s.enviados += len(pend)
	return len(pend), nil
}

type bancada struct {
	h     *hal.Fake
	tela  *display.Fake
	flash *store.MemFlash
	st    *store.Store
	sinc  *sincFalso
	l     *Loop
}

func nova(t *testing.T, ajusta func(*Config)) *bancada {
	t.Helper()
	b := &bancada{
		h:     hal.NewFake(t0),
		tela:  display.NewFake(ui.Largura, ui.Altura),
		flash: store.NewMemFlash(setores, setorBytes),
		sinc:  &sincFalso{},
	}
	st, err := store.Open(b.flash, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}
	b.st = st

	cfg := Padrao()
	cfg.PetID = "11111111-1111-1111-1111-111111111111"
	if ajusta != nil {
		ajusta(&cfg)
	}
	l, err := New(b.h, b.tela, st, b.sinc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	b.l = l
	return b
}

func (b *bancada) passos(t *testing.T, n int) {
	t.Helper()
	for i := range n {
		if err := b.l.Passo(); err != nil {
			t.Fatalf("passo %d: %v", i, err)
		}
	}
}

func (b *bancada) gravacoes() int {
	// SaveState é um Erase num dos dois setores de estado
	return b.flash.Apagamentos[0] + b.flash.Apagamentos[1]
}

// Com memory LCD a imagem fica na tela sem energia. Redesenhar sem mudança
// visível é gasto de bateria puro, e é o erro que o docs/06 corrige em relação
// ao tick de 200 ms da primeira versão do kit.
func TestNaoRedesenhaSemMudancaVisivel(t *testing.T) {
	b := nova(t, nil)
	b.passos(t, 1) // o primeiro frame sempre desenha
	base := b.tela.Shows

	b.passos(t, 12) // uma hora de ociosidade, 5 min por passo
	if extra := b.tela.Shows - base; extra > 1 {
		t.Errorf("uma hora parado gerou %d redesenhos, esperado no máximo 1", extra)
	}
}

// A tela mostra atributo em 0..100. Decaimento que não muda nenhum desses
// inteiros é invisível, e comparar o State cru redesenharia a cada tick.
func TestComparaOVisivelNaoOEstadoCru(t *testing.T) {
	b := nova(t, nil)
	b.passos(t, 1)
	base := b.tela.Shows

	// tempo suficiente pro CarrySec e o LastAtUnix mudarem sempre, mas não o
	// bastante pra mexer num inteiro de 0..100 a cada passo
	b.passos(t, 6)
	if extra := b.tela.Shows - base; extra > 2 {
		t.Errorf("%d redesenhos em 30 min parado: está comparando o State, não a View", extra)
	}
}

func TestBotaoViraEventoNoEstadoENaFila(t *testing.T) {
	b := nova(t, nil)
	b.passos(t, 1)
	antes := b.l.Vista()

	b.h.Agendar(time.Minute, hal.BotaoInteragir)
	b.passos(t, 1)

	if b.l.Vista().Stats.Animo <= antes.Stats.Animo {
		t.Errorf("o bicho não respondeu ao botão: ânimo %d -> %d",
			antes.Stats.Animo, b.l.Vista().Stats.Animo)
	}
	pend, err := b.st.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 1 {
		t.Fatalf("%d eventos na fila de sync, esperado 1", len(pend))
	}
	if pend[0].Kind != sim.KindInteract {
		t.Errorf("kind = %v, esperado KindInteract", pend[0].Kind)
	}
	if len(pend[0].ID) != 26 {
		t.Errorf("id = %q, esperado um ULID de 26 caracteres", pend[0].ID)
	}
}

// Os quatro gatilhos do docs/06: mudança de estágio, botão, bateria crítica e
// pós-sync. Tick ocioso não é um deles.
func TestTickOciosoNaoGravaNaFlash(t *testing.T) {
	b := nova(t, nil)
	b.passos(t, 1)
	antes := b.gravacoes()

	b.passos(t, 200) // ~17 horas paradas, atravessando o sync de 12h
	if depois := b.gravacoes() - antes; depois != 0 {
		t.Errorf("%d gravações em 200 ticks ociosos, esperado 0", depois)
	}
}

func TestBotaoGrava(t *testing.T) {
	b := nova(t, nil)
	b.passos(t, 1)
	antes := b.gravacoes()

	b.h.Agendar(time.Minute, hal.BotaoInteragir)
	b.passos(t, 1)

	if b.gravacoes() == antes {
		t.Error("botão pressionado não gravou o estado")
	}
}

func TestBateriaCriticaGravaUmaVezSo(t *testing.T) {
	b := nova(t, nil)
	b.passos(t, 1)
	antes := b.gravacoes()

	b.h.Nivel = 5
	b.passos(t, 20)

	n := b.gravacoes() - antes
	if n == 0 {
		t.Error("bateria crítica não gravou o estado")
	}
	if n > 1 {
		t.Errorf("bateria crítica gravou %d vezes: vai gravar a cada tick até o device morrer", n)
	}
}

// O modelo de vida útil do store diz 78 anos com 7 saves por dia. O loop tem
// que caber nesse orçamento em uso real, senão a conta não vale nada.
func TestDiaRealistaCabeNoOrcamentoDeFlash(t *testing.T) {
	b := nova(t, nil)

	// cinco interações espalhadas pelo dia
	for i, h := range []int{7, 12, 13, 19, 22} {
		botao := hal.BotaoInteragir
		if i%2 == 0 {
			botao = hal.BotaoAlimentar
		}
		b.h.AgendarEm(t0.Add(time.Duration(h)*time.Hour), botao)
	}

	// um dia inteiro de passos
	fim := t0.Add(24 * time.Hour)
	for b.h.Now().Before(fim) {
		if err := b.l.Passo(); err != nil {
			t.Fatal(err)
		}
	}

	n := b.gravacoes()
	t.Logf("um dia realista: %d gravações de estado, %d sonos, %d redesenhos",
		n, b.h.Sonos, b.tela.Shows)
	if n > 7 {
		t.Errorf("%d gravações num dia, o modelo de vida útil assume no máximo 7", n)
	}
}

// Ocioso acorda a cada 5 min; botão põe o device em modo ativo. O modo ativo
// vem desligado por padrão (ver Config.DuracaoAtiva), então o teste liga
// explicitamente pra exercitar o mecanismo que vai sustentar a animação.
func TestBotaoAcionaModoAtivo(t *testing.T) {
	b := nova(t, func(c *Config) { c.DuracaoAtiva = 15 * time.Second })
	b.passos(t, 1)

	b.h.Agendar(time.Minute, hal.BotaoInteragir)
	b.passos(t, 1)
	depoisDoBotao := b.h.Now()

	b.passos(t, 1)
	espera := b.h.Now().Sub(depoisDoBotao)
	if espera > time.Second {
		t.Errorf("depois do botão o device dormiu %v, esperado o passo ativo (~100ms)", espera)
	}

	// passado o tempo ativo, volta pro intervalo ocioso
	for b.h.Now().Sub(depoisDoBotao) < 20*time.Second {
		b.passos(t, 1)
	}
	antes := b.h.Now()
	b.passos(t, 1)
	if d := b.h.Now().Sub(antes); d < 4*time.Minute {
		t.Errorf("passado o modo ativo o device dormiu %v, esperado ~5min", d)
	}
}

// Fila cheia força sync em vez de perder o evento: evento perdido é treino que
// o dono fez e o bicho não comeu.
func TestFilaCheiaForcaSync(t *testing.T) {
	b := nova(t, nil)

	// enche a fila por fora do loop
	n := 0
	for {
		err := b.st.AppendPending(sim.Event{
			ID: "0000000000000000000000000", At: t0, Kind: sim.KindInteract,
		})
		if err != nil {
			break
		}
		n++
		if n > 1000 {
			t.Fatal("a fila não encheu")
		}
	}
	chamadasAntes := b.sinc.chamadas

	b.h.Agendar(time.Minute, hal.BotaoInteragir)
	b.passos(t, 2)

	if b.sinc.chamadas == chamadasAntes {
		t.Error("a fila cheia não forçou sync")
	}
	pend, err := b.st.Pending()
	if err != nil {
		t.Fatal(err)
	}
	achou := false
	for _, ev := range pend {
		if ev.Kind == sim.KindInteract && len(ev.ID) == 26 {
			achou = true
		}
	}
	if !achou {
		t.Error("o evento do botão foi perdido quando a fila encheu")
	}
}

func TestSyncMarcaOsPendentesEGrava(t *testing.T) {
	b := nova(t, func(c *Config) { c.IntervaloSync = time.Hour })
	b.passos(t, 1)

	b.h.Agendar(time.Minute, hal.BotaoInteragir)
	b.passos(t, 1)
	gravAntes := b.gravacoes()

	// avança até passar a hora do sync
	for b.h.Now().Before(t0.Add(70 * time.Minute)) {
		b.passos(t, 1)
	}

	if b.sinc.chamadas == 0 {
		t.Fatal("o sync não aconteceu no intervalo configurado")
	}
	pend, err := b.st.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 0 {
		t.Errorf("%d eventos continuaram pendentes depois do sync", len(pend))
	}
	if b.gravacoes() == gravAntes {
		t.Error("o pós-sync não gravou o estado")
	}
}

// Sync que falha não pode marcar como enviado: o evento tem que ser tentado de
// novo, e é a idempotência por ULID que torna o reenvio seguro.
func TestSyncQueFalhaNaoPerdePendente(t *testing.T) {
	b := nova(t, func(c *Config) { c.IntervaloSync = time.Hour })
	b.passos(t, 1)
	b.h.Agendar(time.Minute, hal.BotaoInteragir)
	b.passos(t, 1)

	b.sinc.erro = errFalso{}
	for b.h.Now().Before(t0.Add(70 * time.Minute)) {
		b.passos(t, 1)
	}

	pend, err := b.st.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 1 {
		t.Errorf("%d pendentes depois de um sync que falhou, esperado 1", len(pend))
	}
}

type errFalso struct{}

func (errFalso) Error() string { return "sem rede" }

// Reboot: o device volta de onde estava, e o tempo em que ficou desligado
// conta como decaimento. O bicho não congela na gaveta.
func TestRebootRecuperaEDecaiOTempoDesligado(t *testing.T) {
	b := nova(t, nil)
	b.h.Agendar(time.Minute, hal.BotaoAlimentar)
	b.passos(t, 3)
	antes := b.l.Vista()

	// device desligado por dois dias
	h2 := hal.NewFake(b.h.Now().Add(48 * time.Hour))
	tela2 := display.NewFake(ui.Largura, ui.Altura)
	st2, err := store.Open(b.flash, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Padrao()
	cfg.PetID = "11111111-1111-1111-1111-111111111111"
	l2, err := New(h2, tela2, st2, &sincFalso{}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if l2.Estado().Growth != b.l.Estado().Growth {
		t.Errorf("growth = %d depois do reboot, esperado %d: o estado não foi recuperado",
			l2.Estado().Growth, b.l.Estado().Growth)
	}
	if l2.Vista().Stats.Vigor >= antes.Stats.Vigor {
		t.Errorf("vigor %d depois de 2 dias desligado, era %d: o tempo parado não decaiu",
			l2.Vista().Stats.Vigor, antes.Stats.Vigor)
	}

	pend, err := st2.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) == 0 {
		t.Error("os pendentes não sobreviveram ao reboot")
	}
}

// Alocação zero no regime: o tick ocioso é o que roda milhares de vezes por
// dia e não pode acordar o GC.
func TestTickOciosoNaoAloca(t *testing.T) {
	b := nova(t, nil)
	b.passos(t, 2)

	if n := testing.AllocsPerRun(200, func() { _ = b.l.Passo() }); n != 0 {
		t.Errorf("o tick ocioso alocou %v vezes, esperado 0", n)
	}
}

// flashQuePerdeEnergia falha a gravação de ESTADO (setores 0 e 1) sob comando,
// deixando a fila funcionando. É como se a bateria acabasse exatamente no
// instante entre marcar os eventos como enviados e persistir o estado que os
// contém.
type flashQuePerdeEnergia struct {
	*store.MemFlash
	cair bool
}

func (f *flashQuePerdeEnergia) Erase(setor int64) error {
	if f.cair && setor < 2 {
		return errQueda{}
	}
	return f.MemFlash.Erase(setor)
}

func (f *flashQuePerdeEnergia) Write(off int64, p []byte) error {
	if f.cair && off < 2*setorBytes {
		return errQueda{}
	}
	return f.MemFlash.Write(off, p)
}

type errQueda struct{}

func (errQueda) Error() string { return "bateria acabou" }

// MarkSynced descarta os eventos da fila local, e o estado em flash pode ser
// anterior a eles. Se a energia cair ENTRE descartar e gravar, o device
// reboota com estado velho e sem os eventos pra refazer o caminho: o treino
// sumiu do device mesmo já estando no servidor.
//
// Gravar primeiro fecha a janela: se a gravação falha, o MarkSynced nem
// acontece e os eventos continuam pendentes pra próxima tentativa. Reenviar é
// seguro por causa do ULID; perder não é.
func TestQuedaDeEnergiaNoSyncNaoPerdeOEvento(t *testing.T) {
	mem := store.NewMemFlash(setores, setorBytes)
	flash := &flashQuePerdeEnergia{MemFlash: mem}

	st, err := store.Open(flash, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}
	h := hal.NewFake(t0)
	cfg := Padrao()
	cfg.PetID = "11111111-1111-1111-1111-111111111111"
	cfg.IntervaloSync = time.Hour
	sinc := &sincFalso{}
	l, err := New(h, display.NewFake(ui.Largura, ui.Altura), st, sinc, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// A flash de ESTADO está inutilizada desde o começo. Isso simula o caso em
	// que a gravação do botão também não pegou, que é a única situação em que
	// a ordem importa hoje: o botão normalmente grava no mesmo Passo, então o
	// estado em flash já contém o evento antes de qualquer sync.
	//
	// Amanhã isso deixa de ser hipotético: evento de IMU e encontro por BLE
	// vão entrar na fila sem um botão junto pra forçar a gravação.
	flash.cair = true

	h.Agendar(time.Minute, hal.BotaoAlimentar)
	_ = l.Passo()
	growthEsperado := l.Estado().Growth
	if growthEsperado == 0 {
		t.Fatal("o botão não gerou growth: o teste não provaria nada")
	}

	for h.Now().Before(t0.Add(70 * time.Minute)) {
		_ = l.Passo()
	}
	if sinc.chamadas == 0 {
		t.Fatal("o sync não aconteceu")
	}

	// reboot: a energia voltou
	flash.cair = false
	st2, err := store.Open(flash, setorBytes, setores)
	if err != nil {
		t.Fatal(err)
	}
	gravado, errEstado := st2.LoadState()
	pend, err := st2.Pending()
	if err != nil {
		t.Fatal(err)
	}

	// O evento tem que estar em ALGUM dos dois: ou já foldado no estado
	// gravado, ou ainda pendente pra ser reenviado. Sumir dos dois é perda.
	noEstado := errEstado == nil && gravado.Growth >= growthEsperado
	naFila := len(pend) > 0
	if !noEstado && !naFila {
		t.Errorf("o evento sumiu do device: estado em flash tem growth %d (err=%v) "+
			"e a fila tem %d pendentes", gravado.Growth, errEstado, len(pend))
	}
}

// O modo ativo existe pra sustentar animação: o docs/06 fala em 10 fps por 15 s
// quando o dono interage. Enquanto ui.Animate não existir, ele acorda o device
// 150 vezes por botão pra desenhar o mesmo frame.
//
// Este teste mede o custo dos dois jeitos. Ele não julga: serve pra que a
// decisão de ligar ou desligar o modo ativo seja tomada com número.
func TestCustoDoModoAtivoSemAnimacao(t *testing.T) {
	medir := func(duracaoAtiva time.Duration) (sonos, shows int) {
		b := nova(t, func(c *Config) { c.DuracaoAtiva = duracaoAtiva })
		for _, h := range []int{7, 12, 13, 19, 22} {
			b.h.AgendarEm(t0.Add(time.Duration(h)*time.Hour), hal.BotaoInteragir)
		}
		fim := t0.Add(24 * time.Hour)
		for b.h.Now().Before(fim) {
			if err := b.l.Passo(); err != nil {
				t.Fatal(err)
			}
		}
		return b.h.Sonos, b.tela.Shows
	}

	comAtivo, showsCom := medir(15 * time.Second)
	semAtivo, showsSem := medir(0)

	t.Logf("modo ativo LIGADO:   %d acordadas/dia, %d redesenhos", comAtivo, showsCom)
	t.Logf("modo ativo DESLIGADO: %d acordadas/dia, %d redesenhos", semAtivo, showsSem)
	t.Logf("custo: %d acordadas a mais por dia, %d redesenhos a mais",
		comAtivo-semAtivo, showsCom-showsSem)

	if showsCom > showsSem+len([]int{7, 12, 13, 19, 22}) {
		t.Errorf("o modo ativo gerou %d redesenhos a mais: já tem animação?",
			showsCom-showsSem)
	}
}
