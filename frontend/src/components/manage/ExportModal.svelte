<div class="modal" transition:fade>
  <div class="modal-wrapper">
    <Card footer="{true}" footerRight="{true}" fill="{false}">
      <span slot="title">Export Settings</span>

      <div slot="body" class="body-wrapper">
        Are you sure you want to export all data for this server? This will include all settings, blacklist, tags, and transcripts.
        <a id="export_data" style="display:none;"></a>
      </div>

      <div slot="footer" class="footer-wrapper">
        <Button danger={true} on:click={dispatchClose}>Cancel</Button>
        <div style="">
          <Button on:click={dispatchConfirm}>Confirm</Button>
        </div>
      </div>
    </Card>
  </div>
</div>

<div class="modal-backdrop" transition:fade>
</div>

<svelte:window on:keydown={handleKeydown}/>

<script>
    import {createEventDispatcher} from 'svelte';
    import {fade} from 'svelte/transition'
    import Card from "../Card.svelte";
    import Button from "../Button.svelte";
    
    import {setDefaultHeaders} from '../../includes/Auth.svelte'
    import {notifyError, notifySuccess} from "../../js/util";
    import axios from "axios";
    import {API_URL} from "../../js/constants";
    setDefaultHeaders();

    export let guildId;

    const dispatch = createEventDispatcher();

    function dispatchClose() {
        dispatch('close', {});
    }

    // Dispatch with data
    async function dispatchConfirm() {
      const res = await axios.get(`${API_URL}/api/${guildId}/export`);
      if (res.status !== 200) {
          notifyError(`Failed to export settings: ${res.data.error}`);
          return;
      }

      let exportAnchor = document.getElementById('export_data');
      exportAnchor.href = `data:text/json;charset=utf-8,${encodeURIComponent(JSON.stringify(res.data))}`;
      exportAnchor.setAttribute('download', 'export.json');
      exportAnchor.click();
      dispatchClose();
      notifySuccess('Exported settings successfully');
    }

    function handleKeydown(e) {
        if (e.key === "Escape") {
            dispatchClose();
        }
    }
</script>

<style>
    .modal {
        position: absolute;
        top: 0;
        left: 0;
        width: 100%;
        height: 100%;
        z-index: 501;

        display: flex;
        justify-content: center;
        align-items: center;
    }

    .modal-wrapper {
        display: flex;
        width: 40%;
    }

    @media only screen and (max-width: 1280px) {
        .modal-wrapper {
            width: 96%;
        }
    }

    .modal-backdrop {
        position: fixed;
        top: 0;
        left: 0;
        width: 100%;
        height: 100%;
        z-index: 500;
        background-color: #000;
        opacity: .5;
    }

    .body-wrapper {
        display: flex;
        flex-direction: column;
        gap: 4px;
    }

    .footer-wrapper {
        display: flex;
        flex-direction: row;
        gap: 12px;
    }
</style>