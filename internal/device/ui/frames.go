package ui

import "github.com/ale/fera/internal/sim"

// Frames é o catálogo de telas douradas: o que o bicho deve parecer em cada
// situação que importa. O gerador escreve os arquivos, o teste confere.
//
// Os valores de Growth acompanham os limiares calibrados do stageFor: um
// "filhote" com growth de jovem seria dado de teste que não existe em campo.
//
// Mesma disciplina dos golden vectors do sim: o desenho é contrato, e mudar
// contrato é ato deliberado, não efeito colateral de um refactor.
type Frame struct {
	Nome string
	View sim.View
}

func Frames() []Frame {
	return []Frame{
		{"ovo_recem_nascido", sim.View{
			Stage: sim.StageOvo, Trait: sim.TraitNeutro,
			Stats: sim.Stats{Vigor: 5000, Animo: 5000, Saude: 7000, Vinculo: 1000}}},

		{"filhote_comecando", sim.View{
			Stage: sim.StageFilhote, Trait: sim.TraitAgitado, Growth: 60,
			Stats: sim.Stats{Vigor: 6200, Animo: 7000, Saude: 8000, Vinculo: 2500}}},

		{"jovem_largado", sim.View{
			Stage: sim.StageJovem, Trait: sim.TraitTeimoso, Growth: 200,
			Stats: sim.Stats{Vigor: 0, Animo: 0, Saude: 3000, Vinculo: 0}}},

		{"adulto_em_forma", sim.View{
			Stage: sim.StageAdulto, Trait: sim.TraitFerino, Growth: 800,
			Stats: sim.Stats{Vigor: 9200, Animo: 8100, Saude: 8800, Vinculo: 7400}}},

		{"veterano_no_talo", sim.View{
			Stage: sim.StageVeterano, Trait: sim.TraitCalmo, Growth: 2000,
			Stats: sim.Stats{Vigor: sim.Max, Animo: sim.Max, Saude: sim.Max, Vinculo: sim.Max}}},

		// Tudo zerado tem que parecer bicho definhando, não tela quebrada.
		// É por isso que a moldura da barra é desenhada mesmo com valor zero.
		{"adulto_tudo_zero", sim.View{
			Stage: sim.StageAdulto, Trait: sim.TraitNeutro, Growth: 800,
			Stats: sim.Stats{}}},
	}
}
