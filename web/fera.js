// A FERA no navegador.
//
// Toda a lógica está no WASM: este arquivo só desenha o framebuffer, coleta
// clique e guarda o log de eventos. Se você se pegar escrevendo
// `if (vigor < 20) humor = 'triste'` aqui, parou: essa regra mora no Go.

const CHAVE = 'fera.eventos.v1';
const ESCALA = 5;

let mem, buf, ctx, imagem;
let eventos = [];       // o log. É a única coisa persistida.
let nascimento = 0;     // unix do genesis
let agora = 0;          // relógio do bicho, em unix
let ultimoReal = 0;     // performance.now() do último quadro

function carregarLog() {
  try {
    const cru = localStorage.getItem(CHAVE);
    if (!cru) return null;
    const d = JSON.parse(cru);
    if (!Array.isArray(d.eventos)) return null;
    return d;
  } catch { return null; }
}

function salvarLog() {
  try {
    localStorage.setItem(CHAVE, JSON.stringify({ nascimento, agora, eventos }));
  } catch { /* modo privado, sem storage: o bicho vive só nesta sessão */ }
}

// Replay: o estado é DERIVADO do log, nunca guardado. Mesma regra do
// servidor e do device.
function refazer() {
  feraNovo('web', nascimento);
  for (const e of eventos) {
    feraEvento(e.id, e.kind, e.at, e.kcal | 0, e.zone | 0, e.min | 0);
  }
}

function novoID() {
  // Não é ULID de verdade (o device gera o de verdade); aqui só precisa ser
  // único e crescente, que é o que a idempotência do fold usa.
  return String(Date.now()).padStart(13, '0') + Math.random().toString(36).slice(2, 8);
}

function registrar(kind, kcal, zone, min) {
  const e = { id: novoID(), kind, at: Math.floor(agora), kcal, zone, min };
  eventos.push(e);
  feraEvento(e.id, e.kind, e.at, e.kcal, e.zone, e.min);
  salvarLog();
}

// Rotinas: despejam um mês de eventos de uma vez, no passado, e adiantam o
// relógio junto. É a forma de VER o balanceamento — clicar botão com o tempo
// acelerado não dá pra sentir nada, porque um treino leva ~6 dias simulados
// pra decair.
//
// As personas de verdade, que travam a calibragem, vivem em
// internal/sim/balanceamento_test.go. Estas aqui são controle de demo.
const DIA = 86400;

function viverMes(rotina) {
  const inicio = Math.floor(agora);
  for (let d = 0; d < 30; d++) {
    const base = inicio + d * DIA;
    const r = rotina(d);
    if (r.kcal) eventos.push({ id: novoID(), kind: 'effort', at: base + 7 * 3600, kcal: r.kcal, zone: r.zona, min: 0 });
    if (r.dorme) eventos.push({ id: novoID(), kind: 'sleep', at: base + 23 * 3600, kcal: 0, zone: 0, min: r.dorme });
    if (r.interage) eventos.push({ id: novoID(), kind: 'interact', at: base + 20 * 3600, kcal: 0, zone: 0, min: 0 });
  }
  agora = inicio + 30 * DIA;
  refazer();
  salvarLog();
}

const ROTINAS = {
  // dorme e interage todo dia, mas nunca treina: não evolui, por design
  sedentario: () => ({ kcal: 0, dorme: 450, interage: true }),
  constante:  (d) => ({ kcal: [1, 3, 5].includes(d % 7) ? 500 : 0, zona: 3, dorme: 450, interage: true }),
  atleta:     (d) => ({ kcal: d % 7 !== 0 ? 700 : 0, zona: 4, dorme: 480, interage: true }),
  sumiu:      (d) => (d < 14 && [1, 3, 5].includes(d % 7)
                        ? { kcal: 500, zona: 3, dorme: 450, interage: true }
                        : { kcal: 0, dorme: 0, interage: false }),
};

function desenhar(estado) {
  const L = feraLargura, A = feraAltura;
  const stride = (L + 7) >> 3;
  const px = imagem.data;

  for (let y = 0; y < A; y++) {
    for (let x = 0; x < L; x++) {
      const aceso = (buf[y * stride + (x >> 3)] >> (7 - (x & 7))) & 1;
      const i = (y * L + x) * 4;
      // mono: pixel aceso é escuro, igual a um LCD reflexivo
      px[i] = aceso ? 0x1a : 0xb6;
      px[i + 1] = aceso ? 0x1f : 0xc1;
      px[i + 2] = aceso ? 0x14 : 0xa8;
      px[i + 3] = 255;
    }
  }
  ctx.putImageData(imagem, 0, 0);

  document.getElementById('relogio').textContent =
    new Date(agora * 1000).toISOString().slice(0, 16).replace('T', ' ');
  document.getElementById('nEventos').textContent = String(eventos.length);
}

function laco(t) {
  const vel = Number(document.getElementById('velocidade').value);
  if (ultimoReal) agora += ((t - ultimoReal) / 1000) * vel;
  ultimoReal = t;

  const estado = feraQuadro(Math.floor(agora), buf);
  desenhar(estado);
  requestAnimationFrame(laco);
}

async function iniciar() {
  const go = new Go();
  const r = await WebAssembly.instantiateStreaming(fetch('fera.wasm'), go.importObject);
  go.run(r.instance);

  const L = feraLargura, A = feraAltura;
  const tela = document.getElementById('tela');
  tela.width = L; tela.height = A;
  tela.style.width = (L * ESCALA) + 'px';
  tela.style.height = (A * ESCALA) + 'px';
  ctx = tela.getContext('2d');
  imagem = ctx.createImageData(L, A);
  buf = new Uint8Array((L + 7 >> 3) * A);

  const salvo = carregarLog();
  if (salvo) {
    nascimento = salvo.nascimento;
    agora = salvo.agora;
    eventos = salvo.eventos;
  } else {
    nascimento = Math.floor(Date.now() / 1000);
    agora = nascimento;
  }
  refazer();

  document.getElementById('btTreino').onclick = () => {
    const [kcal, zona] = document.getElementById('intensidade').value.split(',').map(Number);
    registrar('effort', kcal, zona, 0);
  };
  document.getElementById('btInteragir').onclick = () => registrar('interact', 0, 0, 0);
  document.getElementById('btDormir').onclick = () => registrar('sleep', 0, 0, 450);
  document.getElementById('btZerar').onclick = () => {
    if (!confirm('Recomeçar do ovo? O log de eventos vai embora.')) return;
    eventos = [];
    nascimento = Math.floor(Date.now() / 1000);
    agora = nascimento;
    salvarLog();
    refazer();
  };

  for (const [chave, id] of [['sedentario','rtSedentario'], ['constante','rtConstante'],
                             ['atleta','rtAtleta'], ['sumiu','rtSumiu']]) {
    document.getElementById(id).onclick = () => viverMes(ROTINAS[chave]);
  }

  addEventListener('keydown', (e) => {
    if (e.key === 'a') document.getElementById('btTreino').click();
    if (e.key === 'i') document.getElementById('btInteragir').click();
    if (e.key === 'd') document.getElementById('btDormir').click();
  });

  setInterval(salvarLog, 2000); // guarda o relógio de vez em quando
  requestAnimationFrame(laco);
}

iniciar();
