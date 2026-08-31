package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/maypok86/otter/v2"
	"golang.org/x/time/rate"
)

// Limite é a cota por device. Zero desliga.
//
// Desligado é o padrão de uma instalação caseira: o docs/01 põe o Caddy na
// frente, e limitar duas vezes só complica o diagnóstico de quem apanhou.
// Ligue quando isto for pra rede.
type Limite struct {
	PorMinuto int
	Rajada    int
}

// LimitePadrao vem do resilience-cache: "100 req/min por device é folgado pra
// um bicho que sincroniza a cada 15min".
func LimitePadrao() Limite { return Limite{PorMinuto: 100, Rajada: 100} }

// rateLimit conta POR DEVICE, não global.
//
// Global seria pior que nada: um device com retry maluco derrubaria o bicho de
// todo mundo, que é exatamente o incidente que o limite deveria impedir.
//
// Os limitadores vivem num otter com TTL, e não num map que só cresce: device
// que apareceu uma vez e sumiu não pode ficar ocupando memória pra sempre.
func rateLimit(l Limite) func(http.Handler) http.Handler {
	if l.PorMinuto <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	if l.Rajada <= 0 {
		l.Rajada = l.PorMinuto
	}

	intervalo := time.Minute / time.Duration(l.PorMinuto)
	cache := otter.Must(&otter.Options[string, *rate.Limiter]{
		MaximumSize: 10_000,
		// TTL bem maior que a janela do limite: expirar cedo demais devolveria
		// a cota cheia pra quem está martelando.
		ExpiryCalculator: otter.ExpiryAccessing[string, *rate.Limiter](30 * time.Minute),
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d, ok := deviceDoCtx(r.Context())
			if !ok {
				// Sem device no contexto o middleware de auth já barrou. Não
				// dá pra limitar quem não se identificou.
				next.ServeHTTP(w, r)
				return
			}

			lim, _ := cache.Compute(d.ID, func(atual *rate.Limiter, achou bool) (*rate.Limiter, otter.ComputeOp) {
				if achou {
					return atual, otter.WriteOp
				}
				return rate.NewLimiter(rate.Every(intervalo), l.Rajada), otter.WriteOp
			})

			if !lim.Allow() {
				// Retry-After em segundos, pro device esperar em vez de
				// martelar. Sem isso, um cliente ingênuo vira ataque.
				w.Header().Set("Retry-After", strconv.Itoa(int(intervalo.Seconds())+1))
				writeErr(w, http.StatusTooManyRequests, codeRateLimit,
					"muitas requisições, tente daqui a pouco")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
