<script lang="ts">
  import { SettingsPane } from '@prusa/connect-ui/primitives';

  import AddMachine from './add-machine.svelte';
  import { heldHost } from './host';

  // A settings pane takes no props. The host draws it on the Extensions
  // screen with no heading, so the pane names itself.
  const t = heldHost().t;
</script>

<SettingsPane
  title={t('machines.settings.title')}
  description={t('machines.settings.body')}
  edged
>
  <AddMachine />
</SettingsPane>
