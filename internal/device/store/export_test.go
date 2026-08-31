package store

// Helpers que só existem pro teste, e por isso moram num arquivo _test: eles
// precisam do layout interno pra simular queda de energia e bit rot, e esse
// layout não deve virar API pública por causa de teste.

// CorromperUltimoEstado estraga o CRC do registro de estado mais recente,
// que é o que sobra de uma escrita interrompida no meio.
func (s *Store) CorromperUltimoEstado(f *MemFlash) bool {
	ultimo := (s.setorEstado + 1) % setoresEstado
	// offEstCRC+1 é o byte baixo do CRC: o alto pode ser 0x00 por acaso, e aí
	// zerar não mudaria nada.
	return f.Corromper(ultimo*s.setorBytes+offEstCRC+1, 0x00) ||
		f.Corromper(ultimo*s.setorBytes+offEstCRC, 0x00)
}

// CorromperPendente zera um byte do corpo do registro n da fila.
//
// Mira no ID e não no timestamp: os quatro bytes altos do At são zero pra
// qualquer data deste século, e zerar byte que já é zero não corrompe nada.
func (s *Store) CorromperPendente(f *MemFlash, n int64) bool {
	return f.Corromper(s.offSlot(n)+offID, 0x00)
}
