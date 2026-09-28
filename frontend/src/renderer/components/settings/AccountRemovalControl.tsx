import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AccountControlError, accountControlMessage, accountRemovalIsActive, changeAccountRemoval,
  fetchAccountRemoval, fetchAccountRemovalImpact, readAccountRemovalReferences, saveAccountRemovalReference, startAccountRemoval,
  type AccountRemovalReference, type AccountRemovalRequest,
} from "../../lib/accounts-manager-controls";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";

const referencesKey = ["accounts-manager", "removal-references"];

export function AccountRemovalControl({ accountId }: { accountId: string }) {
  return <RemovalPanel key={accountId} accountId={accountId} />;
}

function RemovalPanel({ accountId }: { accountId: string }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [initial] = useState(() => {
    try { return { reference: readAccountRemovalReferences().find(value => value.accountId === accountId), unreadable: false }; }
    catch { return { reference: undefined, unreadable: true }; }
  });
  const [reference, setReference] = useState(initial.reference);
  const [localError, setLocalError] = useState(initial.unreadable ? t("accountsManager.controls.removalSavedError") : "");
  const [confirmedRevision, setConfirmedRevision] = useState<number>();
  const inFlight = useRef(false);
  const persist = (next?: AccountRemovalReference) => {
    try {
      client.setQueryData(referencesKey, saveAccountRemovalReference(accountId, next));
      setReference(next);
      return true;
    } catch { setLocalError(t("accountsManager.controls.removalSaveError")); return false; }
  };
  const impactKey = ["accounts-manager", "removal-impact", accountId];
  const operationKey = (id?: string) => ["accounts-manager", "removal", accountId, id];
  const mutation = useMutation({
    mutationFn: (request: AccountRemovalRequest | { operationId: string; action: "retry" | "cancel" }) => "action" in request
      ? changeAccountRemoval(accountId, request.operationId, request.action)
      : startAccountRemoval(accountId, request),
    retry: false,
    onMutate: async request => { await client.cancelQueries({ queryKey: operationKey(request.operationId) }); },
    onSuccess: result => client.setQueryData(operationKey(result.id), result),
    onError: (error, request) => {
      if (!("action" in request) && error instanceof AccountControlError && [400, 409].includes(error.status)) {
        persist(undefined);
        setConfirmedRevision(undefined);
      }
    },
    onSettled: async () => {
      await client.invalidateQueries({ queryKey: impactKey });
      inFlight.current = false;
    },
  });
  const operationQuery = useQuery({
    queryKey: operationKey(reference?.operationId),
    queryFn: ({ signal }) => fetchAccountRemoval(accountId, reference!.operationId, signal),
    enabled: Boolean(reference) && !mutation.isPending,
    retry: false,
    refetchInterval: query => query.state.data && !accountRemovalIsActive(query.state.data) ? false : 1500,
  });
  const operation = operationQuery.data;
  const impactQuery = useQuery({
    queryKey: impactKey,
    queryFn: ({ signal }) => fetchAccountRemovalImpact(accountId, signal),
    enabled: !reference && !localError,
    retry: false,
  });
  const impact = operation?.impact ?? impactQuery.data;
  const busy = mutation.isPending || operationQuery.isFetching || impactQuery.isFetching;
  useEffect(() => {
    if (operation?.phase === "complete") void client.invalidateQueries({ queryKey: ["accounts-manager", "accounts"] });
  }, [client, operation?.id, operation?.phase]);
  const request = (body: AccountRemovalRequest) => {
    if (inFlight.current || busy || localError) return;
    if (!persist({ accountId, operationId: body.operationId, request: body })) return;
    inFlight.current = true;
    mutation.mutate(body);
  };
  const change = (action: "retry" | "cancel") => {
    if (!operation || busy || operationQuery.isError || inFlight.current) return;
    inFlight.current = true;
    mutation.mutate({ operationId: operation.id, action });
  };
  return <div className="space-y-4 text-sm">
    <p className="break-all">{t("accountsManager.controls.account", { id: accountId })}</p>
    <p className="text-xs text-muted-foreground">{t("accountsManager.controls.removalDescription")}</p>
    {localError ? <p role="alert" className="text-destructive">{localError}</p> : null}
    {(reference ? operationQuery.error : impactQuery.error) ? <p role="alert" className="text-destructive">{accountControlMessage(reference ? operationQuery.error : impactQuery.error, t)}</p> : null}
    {mutation.error ? <div role="alert" className="text-destructive"><p>{accountControlMessage(mutation.error, t)}</p><p>{t("accountsManager.controls.submitted", { id: mutation.variables?.operationId })}</p></div> : null}
    {!reference && impactQuery.isPending && !localError ? <p role="status">{t("accountsManager.controls.checkingRemoval")}</p> : null}
    {!reference && impactQuery.isError ? <Button size="sm" variant="outline" disabled={busy} onClick={() => void impactQuery.refetch()}>{t("accountsManager.controls.refreshRemovalCapability")}</Button> : null}
    {impact ? <section aria-label={t("accountsManager.controls.impact")} className="rounded-md border border-border p-3 space-y-2">
      <h4 className="font-medium">{t("accountsManager.controls.impact")}</h4><p>{t("accountsManager.controls.impactRevision", { revision: impact.revision })}</p>
      {impact.sessions.length ? <ul className="space-y-2">{impact.sessions.map(session => <li key={`${session.sessionId}:${session.provider}`} className="break-all">
        {t("accountsManager.controls.impactSession", { id: session.sessionId, provider: session.provider, revision: session.bindingRevision, status: session.stopped ? t("accountsManager.controls.stopAcknowledged") : t("accountsManager.controls.stopUnconfirmed") })}
      </li>)}</ul> : <p>{t("accountsManager.controls.noBindings")}</p>}
    </section> : null}
    {reference && !operation ? <section aria-label={t("accountsManager.controls.unknownRemoval")} className="space-y-2">
      <p className="break-all">{t("accountsManager.controls.unknownRemovalId", { id: reference.operationId })}</p>
      <p>{t("accountsManager.controls.unknownRemovalResult")}</p>
      <Button size="sm" disabled={busy} onClick={() => void operationQuery.refetch()}>{t("accountsManager.controls.checkRemoval")}</Button>
      {reference.request && operationQuery.error instanceof AccountControlError && operationQuery.error.status === 404 ? <Button size="sm" variant="outline" disabled={busy || Boolean(localError)} onClick={() => request(reference.request!)}>{t("accountsManager.controls.resendRemoval")}</Button> : null}
    </section> : null}
    {operation ? <section aria-label={t("accountsManager.controls.removalOperation")} className="space-y-2" aria-live="polite">
      <h4 className="font-medium">{operation.phase === "complete" ? t("accountsManager.controls.removalComplete") : t("accountsManager.controls.removalOperation")}</h4>
      <p className="break-all">{t("accountsManager.controls.operationId", { id: operation.id })}</p><p>{t("accountsManager.controls.phase", { phase: operation.phase })}</p>
      <p>{operation.canCancel ? t("accountsManager.controls.canCancel") : t("accountsManager.controls.cannotCancel")}</p>
      {operation.recoveryRequired ? <p>{t("accountsManager.controls.removalRecoveryRequired")}</p> : null}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={busy} onClick={() => void operationQuery.refetch()}>{t("accountsManager.controls.refreshRemoval")}</Button>
        {operation.phase === "recovery_required" ? <Button size="sm" disabled={busy || operationQuery.isError} onClick={() => change("retry")}>{t("accountsManager.controls.retryRemoval")}</Button> : null}
        {operation.canCancel && accountRemovalIsActive(operation) ? <Button size="sm" variant="outline" disabled={busy || operationQuery.isError} onClick={() => change("cancel")}>{t("accountsManager.controls.cancelRemoval")}</Button> : null}
        {!accountRemovalIsActive(operation) ? <Button size="sm" variant="ghost" disabled={busy} onClick={() => { setConfirmedRevision(undefined); persist(undefined); }}>{t("accountsManager.controls.dismissOperation")}</Button> : null}
      </div>
    </section> : null}
    {!reference && impact && !impactQuery.isError ? <div className="space-y-3">
      <label className="flex items-start gap-2"><input type="checkbox" disabled={busy || Boolean(localError)} checked={confirmedRevision === impact.revision} onChange={event => setConfirmedRevision(event.target.checked ? impact.revision : undefined)} />{t("accountsManager.controls.confirmRemoval", { revision: impact.revision })}</label>
      <div className="flex flex-wrap gap-2">
        <Button disabled={busy || Boolean(localError) || confirmedRevision !== impact.revision} onClick={() => request({ operationId: crypto.randomUUID(), expectedRevision: impact.revision, confirmed: true })}>{t("accountsManager.controls.requestRemoval")}</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => { setConfirmedRevision(undefined); void impactQuery.refetch(); }}>{t("accountsManager.controls.refreshImpact")}</Button>
      </div>
    </div> : null}
  </div>;
}

export function AccountRemovalDialog({ accountId, open, onOpenChange }: { accountId: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useTranslation();
  return <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent className="max-h-[85vh] overflow-y-auto">
      <DialogHeader><DialogTitle>{t("accountsManager.controls.removalTitle")}</DialogTitle><DialogDescription>{t("accountsManager.controls.removalDialogDescription")}</DialogDescription></DialogHeader>
      {open ? <AccountRemovalControl accountId={accountId} /> : null}
    </DialogContent>
  </Dialog>;
}

export function AccountRemovalRecovery() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const references = useQuery({ queryKey: referencesKey, queryFn: readAccountRemovalReferences, retry: false });
  const [selected, setSelected] = useState("");
  const [accountId, setAccountId] = useState("");
  const [operationId, setOperationId] = useState("");
  const [error, setError] = useState("");
  const [checking, setChecking] = useState(false);
  const inspect = async () => {
    if (checking) return;
    setChecking(true); setError("");
    try {
      await fetchAccountRemoval(accountId, operationId);
      const existing = readAccountRemovalReferences().find(value => value.accountId === accountId);
      if (existing && existing.operationId !== operationId) throw new AccountControlError(409);
      client.setQueryData(referencesKey, saveAccountRemovalReference(accountId, existing ?? { accountId, operationId }));
      setSelected(accountId);
    } catch (cause) { setError(accountControlMessage(cause, t)); }
    finally { setChecking(false); }
  };
  return <details className="rounded-md border border-border p-3 text-sm">
    <summary className="cursor-pointer font-medium">{t("accountsManager.controls.recoverySummary", { saved: references.data?.length ?? 0 })}</summary>
    <div className="mt-3 space-y-3">
      <p className="text-xs text-muted-foreground">{t("accountsManager.controls.recoveryDescription")}</p>
      {references.error ? <p role="alert">{t("accountsManager.controls.referencesUnavailable")}</p> : null}
      {references.data?.map(reference => <Button key={reference.accountId} size="sm" variant="outline" className="max-w-full break-all" onClick={() => setSelected(reference.accountId)}>{t("accountsManager.controls.inspectReference", { account: reference.accountId, operation: reference.operationId })}</Button>)}
      <Input aria-label={t("accountsManager.controls.recoveryAccount")} value={accountId} onChange={event => setAccountId(event.target.value)} autoComplete="off" />
      <Input aria-label={t("accountsManager.controls.recoveryRemoval")} value={operationId} onChange={event => setOperationId(event.target.value)} autoComplete="off" />
      <Button size="sm" disabled={checking || !accountId || !operationId} onClick={() => void inspect()}>{t("accountsManager.controls.inspectRemoval")}</Button>
      {error ? <p role="alert">{error}</p> : null}
      {selected ? <AccountRemovalDialog accountId={selected} open onOpenChange={open => { if (!open) setSelected(""); }} /> : null}
    </div>
  </details>;
}
