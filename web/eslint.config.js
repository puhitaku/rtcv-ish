import pluginVue from 'eslint-plugin-vue'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'
import skipFormatting from 'eslint-config-prettier/flat'

export default defineConfigWithVueTs(
  {
    ignores: ['node_modules/**', 'src/api/schema.d.ts', 'test-results/**', 'playwright-report/**'],
  },
  pluginVue.configs['flat/recommended'],
  vueTsConfigs.recommended,
  skipFormatting,
  {
    rules: {
      'vue/multi-word-component-names': 'off',
    },
  },
)
