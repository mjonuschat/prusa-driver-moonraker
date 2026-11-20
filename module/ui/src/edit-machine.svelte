<!--
  The object mapping of one machine. Each role is a Klipper object the driver
  reads: the driver detects it by name unless the endpoint names one.

  An empty field is auto-detect. The word none turns the role off. Anything
  else is the object's name as Klipper lists it, for example
  `temperature_sensor Chamber_Top`.

  Saving replaces the endpoint with the new options. The host dials the
  machine again from the new declaration, so no restart follows.
-->
<script lang="ts">
  import { describeFault } from '@prusa/connect-ui/i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Button,
    Field,
    FaultAlert,
    Input,
  } from '@prusa/connect-ui/primitives';

  import { rolesChanged, rolesOf, updateBody, withRoles } from './endpoint';
  import type { RoleKey, RoleValues } from './endpoint';
  import { heldHost } from './host';
  import {
    detectedOf,
    deviceOf,
    unresolvedOf,
    type Endpoint,
  } from './machines';

  // Klipper's own names for the usual object of each role, shown as examples.
  const EXAMPLES: Partial<Record<RoleKey, string>> = {
    chamber_object: 'temperature_sensor Chamber_Top',
    bed_fans_object: 'fan_generic bed_fans',
    part_fan_object: 'fan',
    heatbreak_fan_object: 'heater_fan hotend_fan',
    filament_object: 'filament_switch_sensor runout',
    chamber_light_object: 'led chamber_led',
    chamber_fan_object: 'fan_generic chamber_fan',
  };
  const HELPED: readonly RoleKey[] = [
    'chamber_object',
    'extra_sensors',
    'extra_fans',
  ];

  const GROUPS: readonly { title: string; keys: readonly RoleKey[] }[] = [
    {
      title: 'machines.group.chamber',
      keys: ['chamber_object', 'chamber_fan_object', 'chamber_light_object'],
    },
    {
      title: 'machines.group.cooling',
      keys: ['heatbreak_fan_object', 'part_fan_object', 'bed_fans_object'],
    },
    {
      title: 'machines.group.other',
      keys: ['extra_fans', 'extra_sensors', 'filament_object'],
    },
  ];

  interface Props {
    machine: Endpoint;
    oncancel: () => void;
    /** Reads the listing again, so the next save carries the new version. */
    onsaved: () => Promise<void>;
  }

  const { machine, oncancel, onsaved }: Props = $props();

  const host = heldHost();
  const t = host.t;

  // The form starts from the endpoint it was opened on and is not reset when
  // the listing reloads under it.
  // svelte-ignore state_referenced_locally
  let values = $state<RoleValues>(rolesOf(machine.options));
  let saving = $state(false);
  let failure = $state<Error | undefined>();
  let unchanged = $state(false);
  // The printer's capability_epoch when the save went through. It moves when
  // the host dials the machine again, which is when the driver checks the
  // names, so a later epoch means the warnings below are about this save.
  let savedEpoch = $state<number | undefined>();
  const device = $derived(deviceOf(machine, host.fleet.devices));
  const detected = $derived(detectedOf(device));
  const missing = $derived(unresolvedOf(device));
  const checked = $derived(
    savedEpoch === undefined ||
      (device?.connected === true && device.capability_epoch > savedEpoch),
  );
  const placeholderFor = (key: RoleKey): string => {
    if (key === 'extra_sensors' || key === 'extra_fans') {
      return t('machines.mapping.none');
    }
    if (detected === undefined) return t('machines.mapping.auto');
    const name = detected[key];

    return name === undefined
      ? t('machines.mapping.nothing')
      : t('machines.mapping.detected', { name });
  };
  const missingFor = (key: RoleKey): string[] =>
    checked
      ? missing
          .filter((miss) => miss.option === key)
          .filter((miss) => values[key].includes(miss.value))
          .map((miss) => miss.value)
      : [];

  const fault = $derived(
    failure === undefined ? undefined : describeFault(failure),
  );

  const set = (key: RoleKey, value: string): void => {
    values[key] = value;
    unchanged = false;
  };

  const save = async (): Promise<void> => {
    if (!rolesChanged(machine.options, values)) {
      failure = undefined;
      unchanged = true;

      return;
    }
    unchanged = false;
    saving = true;
    failure = undefined;
    try {
      const before = device?.capability_epoch;
      await host.client.putConnectorEndpoint(
        {
          path: { name: machine.name },
          header: { 'If-Match': `"${machine.version}"` },
          body: updateBody(machine, withRoles(machine.options, values)),
        },
        host.signal,
      );
      savedEpoch = before ?? 0;
      await onsaved();
    } catch (error) {
      failure = error instanceof Error ? error : new Error(String(error));
    } finally {
      saving = false;
    }
  };
</script>

<div class="@container/map flex max-w-3xl flex-col gap-6" data-part="mapping">
  <div class="flex flex-col gap-6">
    <header class="flex flex-col gap-1">
      <h2 class="text-base font-medium">
        {t('machines.mapping.title', { name: machine.name })}
      </h2>
      <p class="text-sm text-muted-foreground">
        {t('machines.mapping.lead')}
      </p>
    </header>

    {#if fault !== undefined}
      <FaultAlert {fault} />
    {/if}

    {#if unchanged}
      <Alert data-part="save-status">
        <AlertTitle>{t('machines.mapping.unchanged')}</AlertTitle>
      </Alert>
    {:else if savedEpoch !== undefined}
      <Alert
        variant={checked && missing.length > 0 ? 'warning' : 'default'}
        data-part="save-status"
      >
        <AlertTitle>
          {#if !checked}
            {t('machines.mapping.saved.title')}
          {:else if missing.length > 0}
            {t('machines.mapping.missing.title')}
          {:else}
            {t('machines.mapping.found.title')}
          {/if}
        </AlertTitle>
        <AlertDescription>
          {#if !checked}
            {device?.connected === true
              ? t('machines.mapping.saved.checking')
              : t('machines.mapping.saved.offline')}
          {:else if missing.length > 0}
            {t('machines.mapping.missing.body')}
          {:else}
            {t('machines.mapping.found.body')}
          {/if}
        </AlertDescription>
      </Alert>
    {:else if missing.length > 0}
      <Alert variant="warning" data-part="save-status">
        <AlertTitle>{t('machines.mapping.missing.title')}</AlertTitle>
        <AlertDescription>
          {t('machines.mapping.missing.body')}
        </AlertDescription>
      </Alert>
    {/if}

    {#each GROUPS as group (group.title)}
      <fieldset class="flex flex-col gap-4">
        <legend class="pb-2 text-sm font-medium">{t(group.title)}</legend>
        <div class="grid gap-6 @2xl/map:grid-cols-2">
          {#each group.keys as key (key)}
            <div class="flex flex-col gap-2">
              <Field
                label={t(`machines.role.${key}`)}
                {...missingFor(key).length === 0
                  ? {}
                  : {
                      error: t('machines.mapping.field.missing', {
                        names: missingFor(key).join(', '),
                      }),
                    }}
                control={Input}
                controlProps={{
                  value: values[key],
                  'aria-describedby': `role-${key}-help`,
                  placeholder: placeholderFor(key),
                  maxlength: 253,
                  autocomplete: 'off',
                  spellcheck: false,
                  oninput: (event: Event) => {
                    set(key, (event.currentTarget as HTMLInputElement).value);
                  },
                }}
              />
              <p id="role-{key}-help" class="text-sm text-muted-foreground">
                {#if HELPED.includes(key)}
                  {t(`machines.role.${key}.help`)}
                {/if}
                {#if EXAMPLES[key] !== undefined}
                  {t('machines.role.example')}
                  <code class="font-mono text-foreground">{EXAMPLES[key]}</code>
                {/if}
              </p>
            </div>
          {/each}
        </div>
      </fieldset>
    {/each}

    <nav
      aria-label={t('machines.mapping.nav')}
      class="flex items-center justify-between gap-3 border-t border-border pt-4"
    >
      <Button variant="ghost" onclick={oncancel} disabled={saving}>
        {savedEpoch === undefined
          ? t('machines.steps.cancel')
          : t('machines.mapping.back')}
      </Button>
      <Button onclick={() => void save()} disabled={saving} aria-busy={saving}>
        {t('machines.mapping.save')}
      </Button>
    </nav>
  </div>
</div>
