<!--
  Adding a Klipper machine, as the Prusa "Add a printer" guide draws it: the
  steps down the side and the open step in a card beside them.

  Moonraker is reached the other way round from a Prusa printer. This server
  dials it, so there is no settings file. The steps are the address, the
  details a person may change, the wait for the machine to connect, and the
  machine connected.

  The wait reads the library's fleet store, which the host keeps current from
  the event stream. A machine counts as the one just added when a printer with
  the identity written for it is connected.
-->
<script lang="ts">
  import { describeFault } from '@prusa/connect-ui/i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Button,
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
    Field,
    FaultAlert,
    Input,
    StepList,
  } from '@prusa/connect-ui/primitives';
  import { onDestroy } from 'svelte';

  import {
    DEFAULT_CONNECTOR,
    cleanAddress,
    endpointWrite,
    looksLikeAddress,
    nameFromAddress,
    validName,
    type MachineForm,
  } from './endpoint';
  import { heldHost } from './host';

  interface Props {
    /** Leaves the steps without adding. Absent when there is nothing to go back to. */
    oncancel?: () => void;
    /** Leaves the steps after a machine was added. */
    ondone?: () => void;
  }

  const { oncancel, ondone }: Props = $props();

  type Step = 'address' | 'details' | 'waiting' | 'connected';
  const ORDER: readonly Step[] = ['address', 'details', 'waiting', 'connected'];
  const HELP_AFTER_MS = 30_000;

  const host = heldHost();
  const t = host.t;

  let step = $state<Step>('address');
  let form = $state<MachineForm>({
    address: '',
    name: '',
    unitUid: '',
    vendor: '',
    model: '',
    apiKey: '',
  });
  let touchedName = $state(false);
  let showErrors = $state(false);
  let saving = $state(false);
  let failure = $state<Error | undefined>();
  let slow = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;
  let created = $state<{ name: string; version: string } | undefined>();

  const position = $derived(ORDER.indexOf(step));
  const name = $derived(
    created?.name ?? (touchedName ? form.name : nameFromAddress(form.address)),
  );
  const identity = $derived(
    form.unitUid.trim() === '' ? name : form.unitUid.trim(),
  );

  const arrived = $derived(
    host.fleet.devices.find(
      (device) => device.unit_uid === identity && device.connected,
    ),
  );

  // The page follows the machine: once it is connected, the last step opens
  // without a click.
  $effect(() => {
    if (step === 'waiting' && arrived !== undefined) {
      clearTimeout(timer);
      step = 'connected';
    }
  });

  onDestroy(() => {
    clearTimeout(timer);
  });

  const steps = $derived([
    {
      id: 'address',
      label: t('machines.steps.address'),
      description: t('machines.steps.address.caption'),
    },
    {
      id: 'details',
      label: t('machines.steps.details'),
      description: t('machines.steps.details.caption'),
    },
    {
      id: 'waiting',
      label: t('machines.steps.waiting'),
      description: t('machines.steps.waiting.caption'),
    },
    {
      id: 'connected',
      label: t('machines.steps.connected'),
      description: t('machines.steps.connected.caption'),
    },
  ]);

  const addressError = $derived(
    showErrors && !looksLikeAddress(form.address)
      ? t('machines.address.invalid')
      : undefined,
  );
  const nameError = $derived(
    showErrors && !validName(name) ? t('machines.name.invalid') : undefined,
  );

  const next = (): void => {
    if (!looksLikeAddress(form.address)) {
      showErrors = true;

      return;
    }
    showErrors = false;
    failure = undefined;
    step = 'details';
  };

  const back = (): void => {
    failure = undefined;
    slow = false;
    clearTimeout(timer);
    step = ORDER[Math.max(0, position - 1)] ?? 'address';
  };

  const add = async (): Promise<void> => {
    if (!validName(name)) {
      showErrors = true;

      return;
    }
    saving = true;
    failure = undefined;
    try {
      created = await host.client.putConnectorEndpoint(
        endpointWrite({ ...form, name }, created),
        host.signal,
      );
      slow = false;
      step = 'waiting';
      timer = setTimeout(() => {
        slow = true;
      }, HELP_AFTER_MS);
    } catch (error) {
      failure = error instanceof Error ? error : new Error(String(error));
    } finally {
      saving = false;
    }
  };

  const another = (): void => {
    form = {
      address: '',
      name: '',
      unitUid: '',
      vendor: '',
      model: '',
    };
    touchedName = false;
    created = undefined;
    showErrors = false;
    slow = false;
    step = 'address';
  };

  const fault = $derived(
    failure === undefined ? undefined : describeFault(failure),
  );
  const printerHref = $derived(
    arrived === undefined ? '' : `/printers/${encodeURIComponent(arrived.id)}`,
  );
</script>

<div
  class="@container/add flex flex-col gap-4"
  data-part="wizard"
  data-step={step}
>
  <div class="flex flex-col gap-4 @3xl/add:flex-row @3xl/add:gap-8">
    <aside class="hidden w-56 shrink-0 pbs-6 @3xl/add:block">
      <StepList
        label={t('machines.steps.label')}
        {steps}
        current={step === 'connected' ? ORDER.length : position}
        connected
      />
    </aside>

    <p class="text-sm text-muted-foreground @3xl/add:hidden">
      {t('machines.steps.position', {
        current: position + 1,
        total: ORDER.length,
        step: steps[position]?.label ?? '',
      })}
    </p>

    <div class="flex min-w-0 flex-1 flex-col gap-4 @3xl/add:max-w-2xl">
      <div class="flex flex-col gap-6" data-part="step-card">
        <div class="flex flex-col gap-6">
          {#if step === 'address'}
            <header class="flex flex-col gap-1">
              <h2 class="text-base font-medium">
                {t('machines.address.title')}
              </h2>
            </header>
            <Field
              label={t('machines.address.label')}
              description={t('machines.address.help')}
              {...addressError === undefined ? {} : { error: addressError }}
              control={Input}
              controlProps={{
                value: form.address,
                placeholder: 'voron.local:7125',
                autocomplete: 'off',
                spellcheck: false,
                oninput: (event: Event) => {
                  form.address = (
                    event.currentTarget as HTMLInputElement
                  ).value;
                },
                onkeydown: (event: KeyboardEvent) => {
                  if (event.key === 'Enter') next();
                },
              }}
            />
          {:else if step === 'details'}
            <header class="flex flex-col gap-1">
              <h2 class="text-base font-medium">
                {t('machines.details.title')}
              </h2>
            </header>
            {#if fault !== undefined}
              <FaultAlert {fault} />
            {/if}
            <p class="text-sm" data-part="address-summary">
              {t('machines.details.address', {
                address: cleanAddress(form.address),
              })}
            </p>
            <Field
              label={t('machines.name.label')}
              description={t('machines.name.help')}
              {...nameError === undefined ? {} : { error: nameError }}
              control={Input}
              controlProps={{
                value: name,
                disabled: created !== undefined,
                autocomplete: 'off',
                oninput: (event: Event) => {
                  touchedName = true;
                  form.name = (event.currentTarget as HTMLInputElement).value;
                },
              }}
            />
            <Field
              label={t('machines.model.label')}
              description={t('machines.model.help')}
              control={Input}
              controlProps={{
                value: form.model,
                autocomplete: 'off',
                oninput: (event: Event) => {
                  form.model = (event.currentTarget as HTMLInputElement).value;
                },
              }}
            />
            <Collapsible data-part="advanced">
              <CollapsibleTrigger variant="inline">
                {t('machines.advanced')}
              </CollapsibleTrigger>
              <CollapsibleContent class="flex flex-col gap-6 pt-4">
                <Field
                  label={t('machines.unit.label')}
                  description={t('machines.unit.help')}
                  control={Input}
                  controlProps={{
                    value: form.unitUid,
                    placeholder: name,
                    autocomplete: 'off',
                    oninput: (event: Event) => {
                      form.unitUid = (
                        event.currentTarget as HTMLInputElement
                      ).value;
                    },
                  }}
                />
                <Field
                  label={t('machines.vendor.label')}
                  description={t('machines.vendor.help')}
                  control={Input}
                  controlProps={{
                    value: form.vendor,
                    autocomplete: 'off',
                    oninput: (event: Event) => {
                      form.vendor = (
                        event.currentTarget as HTMLInputElement
                      ).value;
                    },
                  }}
                />
                <Field
                  label={t('machines.apiKey.label')}
                  description={t('machines.apiKey.help')}
                  control={Input}
                  controlProps={{
                    value: form.apiKey,
                    type: 'password',
                    autocomplete: 'off',
                    oninput: (event: Event) => {
                      form.apiKey = (
                        event.currentTarget as HTMLInputElement
                      ).value;
                    },
                  }}
                />
              </CollapsibleContent>
            </Collapsible>
          {:else if step === 'waiting'}
            <header class="flex flex-col gap-1">
              <h2 class="text-base font-medium">
                {t('machines.waiting.title')}
              </h2>
              <p class="text-sm text-muted-foreground" role="status">
                {t('machines.waiting.lead', { name })}
              </p>
            </header>
            {#if slow}
              <Alert variant="warning" data-part="waiting-help">
                <AlertTitle>{t('machines.waiting.slow.title')}</AlertTitle>
                <AlertDescription>
                  {t('machines.waiting.slow.body', {
                    address: cleanAddress(form.address),
                    connector: DEFAULT_CONNECTOR,
                  })}
                </AlertDescription>
              </Alert>
            {/if}
          {:else}
            <header class="flex flex-col gap-1">
              <h2 class="text-base font-medium">
                {t('machines.connected.title')}
              </h2>
              <p class="text-sm text-muted-foreground">
                {t('machines.connected.lead', { name: arrived?.name ?? name })}
              </p>
            </header>
            <div class="flex flex-wrap gap-3">
              <Button>
                {#snippet child({ props })}
                  <a {...props} href={printerHref}
                    >{t('machines.connected.open')}</a
                  >
                {/snippet}
              </Button>
              <Button variant="ghost" onclick={another}>
                {t('machines.connected.another')}
              </Button>
              {#if ondone !== undefined}
                <Button variant="ghost" onclick={ondone}>
                  {t('machines.connected.done')}
                </Button>
              {/if}
            </div>
          {/if}

          {#if step === 'address' || step === 'details' || step === 'waiting'}
            <nav
              aria-label={t('machines.steps.nav')}
              class="flex items-center justify-between gap-3 border-t border-border pt-4"
            >
              {#if position > 0}
                <Button variant="ghost" onclick={back} disabled={saving}>
                  {t('machines.steps.back')}
                </Button>
              {:else if oncancel !== undefined}
                <Button variant="ghost" onclick={oncancel}>
                  {t('machines.steps.cancel')}
                </Button>
              {:else}
                <span></span>
              {/if}
              {#if step === 'address'}
                <Button onclick={next}>{t('machines.steps.next')}</Button>
              {:else if step === 'details'}
                <Button
                  onclick={() => void add()}
                  disabled={saving}
                  aria-busy={saving}
                >
                  {t('machines.details.add')}
                </Button>
              {/if}
            </nav>
          {/if}
        </div>
      </div>
    </div>
  </div>
</div>
