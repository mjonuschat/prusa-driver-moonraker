<!--
  The Moonraker pane of the Extensions screen. With no machine it shows the
  empty card the Prusa page shows, with a button that opens the add-machine
  steps. With machines it lists them, and the same button sits under the table.
-->
<script lang="ts">
  import { describeFault } from '@prusa/connect-ui/i18n';
  import {
    Button,
    EmptyState,
    FaultAlert,
    SettingsPane,
    Spinner,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '@prusa/connect-ui/primitives';
  import PencilIcon from '@lucide/svelte/icons/pencil';
  import PlusIcon from '@lucide/svelte/icons/plus';
  import PrinterIcon from '@lucide/svelte/icons/printer';
  import { onMount } from 'svelte';

  import AddMachine from './add-machine.svelte';
  import EditMachine from './edit-machine.svelte';
  import { heldHost } from './host';
  import { deviceOf, moonrakerEndpoints, type Endpoint } from './machines';

  const host = heldHost();
  const t = host.t;

  let machines = $state<Endpoint[]>([]);
  let loaded = $state(false);
  let failure = $state<Error | undefined>();
  let adding = $state(false);
  let editing = $state<string | undefined>();
  const editedMachine = $derived(
    machines.find((machine) => machine.name === editing),
  );

  const load = async (): Promise<void> => {
    try {
      const listing = await host.client.listConnectorEndpoints({}, host.signal);
      machines = moonrakerEndpoints(listing.items);
      failure = undefined;
    } catch (error) {
      failure = error instanceof Error ? error : new Error(String(error));
    } finally {
      loaded = true;
    }
  };

  onMount(() => {
    void load();
  });

  // The mapping stays open after a save, so the person sees what the machine
  // made of it. The listing is read again for the version the next save needs.
  const saved = (): Promise<void> => load();

  const finished = (): void => {
    adding = false;
    void load();
  };

  const fault = $derived(
    failure === undefined ? undefined : describeFault(failure),
  );

  // The dot says the state at a glance and the word says it to a screen reader.
  const statusOf = (endpoint: Endpoint): { label: string; dot: string } => {
    if (!endpoint.enabled) {
      return {
        label: t('machines.table.status.off'),
        dot: 'bg-disabled-foreground',
      };
    }
    const device = deviceOf(endpoint, host.fleet.devices);
    if (device === undefined) {
      return { label: t('machines.table.status.waiting'), dot: 'bg-warning' };
    }

    return device.connected
      ? { label: t('machines.table.status.connected'), dot: 'bg-success' }
      : {
          label: t('machines.table.status.disconnected'),
          dot: 'bg-destructive',
        };
  };

  // The model the operator wrote, else the one the printer reported.
  const modelOf = (endpoint: Endpoint): string => {
    const written = endpoint.options?.model;
    if (written !== undefined && written !== '') return written;

    return deviceOf(endpoint, host.fleet.devices)?.model ?? '';
  };
</script>

<SettingsPane title={t('machines.settings.title')} edged>
  {#if adding}
    <AddMachine oncancel={finished} ondone={finished} />
  {:else if editedMachine !== undefined}
    <EditMachine
      machine={editedMachine}
      oncancel={() => (editing = undefined)}
      onsaved={saved}
    />
  {:else if !loaded}
    <div class="flex justify-center py-8" data-part="loading" role="status">
      <Spinner />
      <span class="sr-only">{t('machines.table.loading')}</span>
    </div>
  {:else if fault !== undefined}
    <FaultAlert {fault} />
  {:else if machines.length === 0}
    <EmptyState
      icon={PrinterIcon}
      heading={t('machines.empty.title')}
      headingLevel="h2"
      data-part="empty"
    >
      {#snippet action()}
        <Button onclick={() => (adding = true)}>
          <PlusIcon />
          {t('machines.add')}
        </Button>
      {/snippet}
    </EmptyState>
  {:else}
    <div class="flex flex-col gap-4" data-part="machines">
      <div class="-mx-2">
        <Table>
          <TableHeader>
            <TableRow class="bg-accent hover:bg-accent">
              <TableHead class="text-sm! font-semibold! text-foreground!">
                {t('machines.table.name')}
              </TableHead>
              <TableHead class="text-sm! font-semibold! text-foreground!">
                {t('machines.table.address')}
              </TableHead>
              <TableHead class="text-sm! font-semibold! text-foreground!">
                {t('machines.table.model')}
              </TableHead>
              <TableHead class="text-sm! font-semibold! text-foreground!">
                {t('machines.table.status')}
              </TableHead>
              <TableHead class="w-12">
                <span class="sr-only">{t('machines.table.actions')}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each machines as machine (machine.name)}
              <TableRow>
                <TableCell>{machine.name}</TableCell>
                <TableCell class="font-mono text-sm"
                  >{machine.address}</TableCell
                >
                <TableCell>{modelOf(machine)}</TableCell>
                <TableCell>
                  <span class="inline-flex items-center gap-2">
                    <span
                      class={[
                        'size-2 shrink-0 rounded-full',
                        statusOf(machine).dot,
                      ]}
                      aria-hidden="true"
                    ></span>
                    {statusOf(machine).label}
                  </span>
                </TableCell>
                <TableCell class="text-end">
                  <Button
                    variant="ghost"
                    size="icon-md"
                    label={t('machines.table.edit', { name: machine.name })}
                    onclick={() => (editing = machine.name)}
                  >
                    <PencilIcon />
                  </Button>
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      </div>
      <div class="flex justify-end border-t border-border pt-4">
        <Button onclick={() => (adding = true)}>
          <PlusIcon />
          {t('machines.add')}
        </Button>
      </div>
    </div>
  {/if}
</SettingsPane>
