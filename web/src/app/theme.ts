import { createTheme, type MantineColorsTuple } from '@mantine/core';

const FONT_FAMILY =
  "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans SC', 'PingFang SC', 'Microsoft YaHei', sans-serif";

const brand: MantineColorsTuple = [
  '#eef3ff', '#dce4f5', '#b9c7e2', '#94a8d0', '#748dc1',
  '#5f7cb8', '#5474b4', '#44639f', '#39588f', '#2d4b81',
];

/** Mantine 主题：统一字体、圆角和组件默认值，页面不再写一次性样式。 */
export const mantineTheme = createTheme({
  primaryColor: 'blue',
  colors: { brand },
  fontFamily: FONT_FAMILY,
  headings: { fontFamily: FONT_FAMILY, fontWeight: '600' },
  defaultRadius: 'md',
  cursorType: 'pointer',
  components: {
    Button: { defaultProps: { size: 'sm' } },
    TextInput: { defaultProps: { size: 'sm' } },
    PasswordInput: { defaultProps: { size: 'sm' } },
    NumberInput: { defaultProps: { size: 'sm' } },
    Select: { defaultProps: { size: 'sm', allowDeselect: false, comboboxProps: { withinPortal: true } } },
    MultiSelect: { defaultProps: { size: 'sm' } },
    Textarea: { defaultProps: { size: 'sm' } },
    Paper: { defaultProps: { withBorder: true, radius: 'md' } },
    Table: { defaultProps: { verticalSpacing: 'sm', horizontalSpacing: 'md', highlightOnHover: true } },
    Badge: { defaultProps: { variant: 'light', radius: 'sm' } },
    Modal: { defaultProps: { centered: true, overlayProps: { backgroundOpacity: 0.35, blur: 2 } } },
  },
});
