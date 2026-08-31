package hal

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)

// O Fake é a base de todo teste do loop. Se ele mentir sobre o relógio ou
// sobre quando um botão acorda o device, tudo construído em cima passa sem
// testar nada. Por isso ele tem teste próprio.

func TestFakeAvancaORelogioAoDormir(t *testing.T) {
	f := NewFake(t0)

	if got := f.Sleep(2 * time.Hour); got != MotivoTimer {
		t.Errorf("motivo = %v, esperado MotivoTimer", got)
	}
	if got := f.Now(); !got.Equal(t0.Add(2 * time.Hour)) {
		t.Errorf("relógio em %v, esperado %v", got, t0.Add(2*time.Hour))
	}
}

// Botão agendado tem que INTERROMPER o sono, não esperar o timer. É esse o
// comportamento do wake por interrupção no ESP32, e é o que faz o bicho
// responder na hora em vez de daqui a 5 minutos.
func TestBotaoInterrompeOSono(t *testing.T) {
	f := NewFake(t0)
	f.Agendar(30*time.Second, BotaoInteragir)

	if got := f.Sleep(5 * time.Minute); got != MotivoBotao {
		t.Fatalf("motivo = %v, esperado MotivoBotao", got)
	}
	if got := f.Now(); !got.Equal(t0.Add(30 * time.Second)) {
		t.Errorf("acordou em %v, esperado %v (o instante do botão)", got, t0.Add(30*time.Second))
	}
	if b := f.Botoes(); len(b) != 1 || b[0] != BotaoInteragir {
		t.Errorf("botões = %v, esperado [BotaoInteragir]", b)
	}
}

// Botão agendado DEPOIS do fim do sono não pode ser antecipado.
func TestBotaoDepoisDoTimerNaoAntecipa(t *testing.T) {
	f := NewFake(t0)
	f.Agendar(10*time.Minute, BotaoInteragir)

	if got := f.Sleep(5 * time.Minute); got != MotivoTimer {
		t.Fatalf("motivo = %v, esperado MotivoTimer", got)
	}
	if !f.Now().Equal(t0.Add(5 * time.Minute)) {
		t.Errorf("acordou em %v, esperado %v", f.Now(), t0.Add(5*time.Minute))
	}
	if b := f.Botoes(); len(b) != 0 {
		t.Errorf("botões = %v, esperado nenhum: o aperto ainda não aconteceu", b)
	}
}

// Botoes drena: ler duas vezes não pode entregar o mesmo aperto duas vezes,
// senão o loop geraria dois eventos pro mesmo toque.
func TestBotoesDrena(t *testing.T) {
	f := NewFake(t0)
	f.Agendar(time.Second, BotaoAlimentar)
	f.Sleep(time.Minute)

	if b := f.Botoes(); len(b) != 1 {
		t.Fatalf("primeira leitura devolveu %v", b)
	}
	if b := f.Botoes(); len(b) != 0 {
		t.Errorf("segunda leitura devolveu %v, esperado vazio", b)
	}
}

// Vários botões dentro da mesma janela de sono saem na ordem em que
// aconteceram.
func TestVariosBotoesSaemEmOrdem(t *testing.T) {
	f := NewFake(t0)
	f.Agendar(3*time.Second, BotaoMenu)
	f.Agendar(1*time.Second, BotaoAlimentar)
	f.Agendar(2*time.Second, BotaoInteragir)

	// cada Sleep para no próximo botão
	var vistos []Botao
	for range 3 {
		f.Sleep(time.Minute)
		vistos = append(vistos, f.Botoes()...)
	}

	want := []Botao{BotaoAlimentar, BotaoInteragir, BotaoMenu}
	if len(vistos) != len(want) {
		t.Fatalf("vistos = %v, esperado %v", vistos, want)
	}
	for i := range want {
		if vistos[i] != want[i] {
			t.Errorf("botão %d = %v, esperado %v", i, vistos[i], want[i])
		}
	}
}

func TestFakeContaOsSonos(t *testing.T) {
	f := NewFake(t0)
	for range 5 {
		f.Sleep(time.Minute)
	}
	if f.Sonos != 5 {
		t.Errorf("Sonos = %d, esperado 5", f.Sonos)
	}
	if f.TempoDormido != 5*time.Minute {
		t.Errorf("TempoDormido = %v, esperado 5m", f.TempoDormido)
	}
}

func TestBateriaEhControlavel(t *testing.T) {
	f := NewFake(t0)
	if f.Bateria() != 100 {
		t.Errorf("bateria inicial = %d, esperado 100", f.Bateria())
	}
	f.Nivel = 7
	if f.Bateria() != 7 {
		t.Errorf("bateria = %d, esperado 7", f.Bateria())
	}
}
