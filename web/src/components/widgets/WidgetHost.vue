<script setup lang="ts">
import type { Component } from 'vue'
import type { Widget, WidgetType } from '@/api'
import ErrorBoundary from '@/components/ui/ErrorBoundary.vue'
import WidgetChart from './WidgetChart.vue'
import WidgetClock from './WidgetClock.vue'
import WidgetLink from './WidgetLink.vue'
import WidgetLinks from './WidgetLinks.vue'
import WidgetNote from './WidgetNote.vue'
import WidgetNumber from './WidgetNumber.vue'
import WidgetStatus from './WidgetStatus.vue'

defineProps<{ widget: Widget }>()

const components: Record<WidgetType, Component> = {
  link: WidgetLink,
  links: WidgetLinks,
  clock: WidgetClock,
  note: WidgetNote,
  number: WidgetNumber,
  chart: WidgetChart,
  status: WidgetStatus,
}
</script>

<template>
  <ErrorBoundary :key="widget.id">
    <component :is="components[widget.type]" :widget="widget" />
  </ErrorBoundary>
</template>
