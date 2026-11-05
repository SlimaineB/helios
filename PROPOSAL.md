# Helios — Évaluation honnête & plan de mise en production

> Draft pour proposition interne. Réévaluer après chaque phase.

## Verdict

**MVP très correct qui démontre un vrai engineering, mais pas un produit fini d'entreprise.**
L'architecture et le cœur de métier sont solides ; il manque des fondamentaux (tests, sécurité,
rétention) avant une proposition sérieuse. Une fois la phase 1-2 faite, c'est présentable.

## Points forts

- **Modèle pull agent → serveur** : agents derrière NAT/firewall, zéro port entrant, un binaire Go
  léger par agent. C'est le vrai différentiateur face à Tower/AWX et le point clé pour des flottes
  Ansible brownfield sans supervision d'état.
- **Sobre** : serveur Go + SQLite, agent Go autonome, docker-compose pour le lab, aucune
  dépendance k8s, aucune offre payante.
- **Fonctions réelles et fonctionnelles** : détection de drift via `ansible --check --diff` +
  extraction diff/resources, autoheal avec cooldown et dédup des runs, commandes asynchrones
  check/apply, tokens de bootstrap, registry de repos (creds git résolues côté serveur, jamais
  exposées par l'API operator).
- **Décisions motivées documentées** : throttle git, jitter, conséquence quota des forges,
  limite SQLite single-writer — matière déjà prête pour les questions en réunion.

## Points faibles (par ordre de gravité)

1. **Zéro test.** `go test ./...` ne contient aucun fichier de test. Premier point demandé dans
   toute review d'outil d'ops. Rédhibitoire pour un lancement sérieux.
2. **Sécurité** :
   - API operator **sans authentification** : n'importe qui dans le réseau peut supprimer des
     instances/applications. Pas de RBAC.
   - Tokens agents long-lived, stockés en clair (SQLite), pas de rotation ni de scoping.
   - Les `output`/`output_tail` des runs peuvent contenir des secrets, persistés et affichés
     sans nettoyage.
   - Creds git en clair en base (masquées à l'API, mais pas au repos).
3. **Pas de rétention / GC** : `runs` et `commands` grossissent sans limite → SQLite gonfle,
   UI ralentit (endpoints operator déjà en N+1).
3b. **SQLite = imposable en production** : single-writer (`SetMaxOpenConns(1)`), pas un SGBD
   multi-concurrents. Un vrai déploiement doit migrer sur **PostgreSQL** — l'interface `Store`
   (server/internal/store/store.go) a été conçue pour ce swap, mais rien n'est implémenté.
4. **Garanties ops absentes** : pas de verrou anti-concours sur Sync (double-clic → commandes en
   double), pas de webhook/notifications, pas de métriques, pas de versioning/upgrade d'agents.
4b. **Pas de fenêtre de maintenance / freeze** : rien n'empêche un apply (manuel ou autoheal)
   pendant les heures de trading. Pour un contexte finance, il faut un mécanisme de
   maintenance window (ex. pas de sync entre 09:00-17:30 sur les apps marquées) qui fige
   autoheal + commandes apply, tout en gardant les checks en lecture seule.
5. **Dépendance à la discipline playbook** : sans `creates:`/`changed_when`, les modules
   `command`/`shell` rapportent `changed` en `--check` → faux drift permanent et boucle autoheal.
   Le détecteur est correct uniquement sur des tâches idempotentes (ou marqueurs explicites).
6. **Zéro CI / repro** : pas de pipeline, pas de lint, pas de Makefile, pas de release taggée.
7. **Packaging** : docker-compose uniquement — pas de systemd, pas de guide d'upgrade d'agents,
   pas de stratégie de versionning.
8. **Docs** : README correct, mais pas de référence API, pas de modèle de sécurité documenté,
   pas de runbook.

## Ce qu'on vend

**Positionnement :** « Détection automatique de drift + synchro pour flottes Ansible legacy,
pull-based, légère, sans Tower/AWX ». Pas un concurrent d'ArgoCD (pas de k8s, pas de rollback /
hooks / notifications / SSO). Le créneau est réel : des équipes avec des serveurs Ansible
orphelins, sans supervision d'état, sans compter un agent léger.

Points d'argumentation déjà prêts :
- Comment ça scale : ~300-1000 agents en SQLite, **PostgreSQL requis au-delà** (backend Store
  à implémenter), poll long pour ×6-12 de capacité.
- Pression git : régime permanent ~1 req/fetch, le vrai risque est l'effet de troupeau (jitter) et
  le quota des forges (cache central, pattern ArgoCD repo-server).

## Plan de mise en production

### Phase 1 — Crédibilité
- Tests Go : store (CRUD/migrations), handlers API (status/checkin/commandes), parsing drift
  (ansible/format).
- CI minimal : build + `go vet` + `go test` (GitHub Actions ou équivalent), lint.
- Makefile (build/lint/test/images), README « Sécurité & Auth ».

### Phase 2 — Sécurité
- Auth opérateur par token simple + RBAC minimal (admin / read-only).
- Chiffrement des secrets repo/token (AES-GCM, clé serveur).
- Masquage des secrets dans les outputs de runs à l'affichage.

### Phase 3 — Rétention & robustesse
- **Migration PostgreSQL** : implémentation du backend `Store` sur Postgres (interface existante),
  migrations SQL gérées — prérequis pour toute prod à plus de ~300 agents.
- GC des runs/commands (TTL configurable).
- 409 anti-double-command tant qu'une commande est pending.
- Payload de checkin allégé.

### Phase 4 — Deliverability
- systemd units + guide d'upgrade d'agents, packaging (deb/tarball binaire).
- Notifications webhook (Slack/Teams/Generic), métriques basiques.
- **Maintenance window / freeze** : créneaux par app (ou global) pendant lesquels les apply et
  l'autoheal sont bloqués, les checks restant actifs (détection seule). Requis pour la finance
  (heures de trading).

## Porte d'entrée (à préciser)
- **Présentation technique (démo lab) :** prêt, phase 1 recommandée avant.
- **Pilote sur une poignée de machines :** exiger phase 1 + 2.