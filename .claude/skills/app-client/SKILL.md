---
name: app-client
description: App cliente da FERA em Expo/React Native. Carregue ao trabalhar em tela, estado do app, integração com Health/Strava, ou ao compilar o core em WASM. Cobre a stack, a regra de não reimplementar lógica e o suporte multiplataforma.
---

# App

## Stack

- **Expo (React Native) + TypeScript.** iOS, Android e web numa base.
  Roda tudo do MacBook M1. Build de iOS via EAS, sem precisar mexer no Xcode.
- Estado: `zustand`. Não use Redux num app com 5 telas.
- Rede: `fetch` + TanStack Query. Cache e retry vêm de graça.
- Storage: `expo-secure-store` pro token, `expo-sqlite` pro log local.

## Regra absoluta: o app não reimplementa a lógica

O `internal/sim` compila pra WASM com TinyGo:

```bash
tinygo build -o web/sim.wasm -target=wasm ./internal/sim/wasm
```

O app carrega o wasm e chama `fold(stateJSON, eventsJSON, nowISO)`.
Se você se pegar escrevendo `if (vigor < 20) mood = 'triste'` em TypeScript,
parou tudo. Essa regra mora no Go.

Fallback se o WASM pesar demais no RN: o app só mostra o snapshot do servidor
e não simula localmente. Perde offline no app, mas o **device** continua offline-first,
que é o que importa. O app é conveniência, não o produto.

## Telas (são cinco)

1. **Bicho** — o estado agora, animado. Tela padrão.
2. **Alimentar** — conectar fonte de esforço, ver o que entrou hoje.
3. **Linha do tempo** — histórico de eventos legível.
4. **Encontros** — quem sua FERA já conheceu por BLE.
5. **Ajustes** — device, conta, exportar dados.

Se aparecer uma sexta tela, questione.

## Integrações de esforço

| Fonte | Como | Prioridade |
|---|---|---|
| Apple HealthKit | `expo-health` / módulo nativo | alta, é o dono de iPhone |
| Health Connect (Android) | módulo nativo | alta |
| Strava | OAuth + webhook no backend | média |
| Manual | usuário digita o treino | baixa, mas necessária pro dia 1 |
| IMU do device | passos e movimento direto | é o diferencial, mas é fase 4 |

Comece pelo manual. Ele destrava o teste do sistema inteiro sem depender
de aprovação de API de ninguém.

## Acessibilidade e multiplataforma

- Tudo funcional sem som. O bicho comunica por forma, não só por beep.
- Web build precisa funcionar, é a demo que você manda pros amigos sem instalar nada.
- Testar em: iPhone, um Android barato, e Safari. O Android barato é o que
  vai revelar todos os problemas de performance.
