import { Badge, Button, createTheme, Input, rem, type CSSVariablesResolver } from '@mantine/core';

const FONT_FAMILY =
  "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'PingFang SC', 'Noto Sans SC', 'Microsoft YaHei', sans-serif";

/** 默认尺寸（sm）的控件高度：按钮和输入框同高，点击区域不小于 40px。 */
const CONTROL_HEIGHT = rem(40);
const isDefaultSize = (size: unknown) => size === undefined || size === 'sm';

/**
 * Mantine 主题：全站唯一的字号、字重、间距和组件默认值来源。
 *
 * 字号五级：xs 13（辅助信息下限）· sm 14（表格、表单、正文）· md 16 · 标题 h4 18 / h3 20 / h2 28。
 * 字重只有 400 和 600；间距按 8px 栅格（xs 8 · sm 12 · md 16 · lg 24 · xl 32）。
 */
export const mantineTheme = createTheme({
  primaryColor: 'blue',
  black: '#212529',
  fontFamily: FONT_FAMILY,
  fontSizes: { xs: rem(13), sm: rem(14), md: rem(16), lg: rem(18), xl: rem(20) },
  lineHeights: { xs: '1.5', sm: '1.55', md: '1.6', lg: '1.6', xl: '1.6' },
  fontWeights: { regular: '400', medium: '600', bold: '600' },
  spacing: { xs: rem(8), sm: rem(12), md: rem(16), lg: rem(24), xl: rem(32) },
  headings: {
    fontFamily: FONT_FAMILY,
    fontWeight: '600',
    sizes: {
      h1: { fontSize: rem(32), lineHeight: '1.25' },
      h2: { fontSize: rem(28), lineHeight: '1.3' },
      h3: { fontSize: rem(20), lineHeight: '1.4' },
      h4: { fontSize: rem(18), lineHeight: '1.45' },
      h5: { fontSize: rem(16), lineHeight: '1.5' },
      h6: { fontSize: rem(14), lineHeight: '1.5' },
    },
  },
  defaultRadius: 'md',
  cursorType: 'pointer',
  components: {
    Button: Button.extend({
      defaultProps: { size: 'sm' },
      vars: (_, props) => ({
        root: isDefaultSize(props.size) ? { '--button-height': CONTROL_HEIGHT, '--button-padding-x': rem(16) }
          // 行内紧凑按钮保留较小的水平内边距，但点击高度同样不小于 40px。
          : props.size === 'compact-sm' ? { '--button-height': CONTROL_HEIGHT, '--button-padding-x': rem(12) }
          : {},
      }),
    }),
    Input: Input.extend({
      vars: (_, props) => ({ wrapper: isDefaultSize(props.size) ? { '--input-height': CONTROL_HEIGHT } : {} }),
    }),
    TextInput: { defaultProps: { size: 'sm' } },
    PasswordInput: { defaultProps: { size: 'sm' } },
    NumberInput: { defaultProps: { size: 'sm' } },
    Select: { defaultProps: { size: 'sm', allowDeselect: false, comboboxProps: { withinPortal: true } } },
    MultiSelect: { defaultProps: { size: 'sm' } },
    Textarea: { defaultProps: { size: 'sm' } },
    InputWrapper: { styles: { label: { marginBottom: rem(4) }, description: { fontSize: 'var(--mantine-font-size-xs)' } } },
    ActionIcon: { defaultProps: { size: 'lg' } },
    Pagination: { defaultProps: { size: 'lg' } },
    Paper: { defaultProps: { withBorder: true, radius: 'md' } },
    Table: {
      defaultProps: { verticalSpacing: 'sm', horizontalSpacing: 'md', highlightOnHover: true },
      styles: { th: { fontSize: 'var(--mantine-font-size-sm)', color: 'var(--mantine-color-dimmed)' } },
    },
    // 状态标签：13px、不大写，比 Mantine 默认的 11px 大写更易读。
    Badge: Badge.extend({
      defaultProps: { variant: 'light', radius: 'sm' },
      vars: (_, props) => ({
        root: props.size === undefined || props.size === 'md'
          ? { '--badge-fz': 'var(--mantine-font-size-xs)', '--badge-height': rem(24), '--badge-padding-x': rem(8) }
          : {},
      }),
      styles: { root: { textTransform: 'none' } },
    }),
    NavLink: { styles: { root: { minHeight: CONTROL_HEIGHT }, label: { fontSize: 'var(--mantine-font-size-sm)' } } },
    Modal: { defaultProps: { centered: true, padding: 'lg', overlayProps: { backgroundOpacity: 0.35, blur: 2 } } },
  },
});

/** 次要文字使用 gray.7（对白底约 8:1），默认的 gray.6 对比度不足 4.5:1。 */
export const cssVariablesResolver: CSSVariablesResolver = () => ({
  variables: {},
  light: { '--mantine-color-dimmed': 'var(--mantine-color-gray-7)' },
  dark: {},
});
