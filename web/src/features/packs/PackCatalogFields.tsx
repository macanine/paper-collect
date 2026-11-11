import { Autocomplete, MultiSelect, Select, SimpleGrid, TagsInput, TextInput } from '@mantine/core';
import type { UseFormReturnType } from '@mantine/form';
import type { DocType, Grade, Pack, PackFacets, PackUpdate, SubjectCode, Term } from '../../shared/api/types';
import { FIELD_LABELS } from '../../shared/lib/labels';
import { DOC_TYPE_OPTIONS, GRADE_OPTIONS, SUBJECT_OPTIONS, TERM_OPTIONS, cohortOptions } from '../../shared/lib/options';

/** 表单中的编目值：空值用 undefined，提交时再转回接口约定的空字符串/0。 */
export interface CatalogFormValues {
  title?: string;
  doc_type?: DocType;
  subject_codes?: SubjectCode[];
  grade?: Grade;
  term?: Term;
  cohort?: number;
  region?: string;
  organization?: string;
  exam_date?: string;
  exam_name?: string;
  chapter?: string;
  tags?: string[];
}

export type CatalogField = Exclude<keyof CatalogFormValues, 'title'>;

const ALL_FIELDS: CatalogField[] = ['doc_type', 'subject_codes', 'grade', 'cohort', 'term', 'region', 'organization', 'exam_date', 'exam_name', 'chapter', 'tags'];

/** 固定地区列表，与服务端 catalogai.Regions 一致；地区只能从中选择。 */
export const REGIONS = [
  '全国甲卷', '全国乙卷', '新高考I卷', '新高考II卷', '全国卷',
  '北京', '天津', '河北', '山西', '内蒙古', '辽宁', '吉林', '黑龙江', '上海', '江苏',
  '浙江', '安徽', '福建', '江西', '山东', '河南', '湖北', '湖南', '广东', '广西',
  '海南', '重庆', '四川', '贵州', '云南', '西藏', '陕西', '甘肃', '青海', '宁夏',
  '新疆', '香港', '澳门', '台湾',
];

export function packToForm(pack: Pack): CatalogFormValues {
  return {
    title: pack.locked_fields.includes('title') ? pack.title : undefined,
    doc_type: pack.doc_type || undefined,
    subject_codes: pack.subject_codes,
    grade: pack.grade || undefined,
    term: pack.term || undefined,
    cohort: pack.cohort || undefined,
    region: pack.region || undefined,
    organization: pack.organization || undefined,
    exam_date: pack.exam_date || undefined,
    exam_name: pack.exam_name || undefined,
    chapter: pack.chapter || undefined,
    tags: pack.tags,
  };
}

/** 只提交表单里出现过的字段；清空的字段提交空值。 */
export function formToUpdate(values: CatalogFormValues, fields: (CatalogField | 'title')[]): PackUpdate {
  const out: PackUpdate = {};
  for (const field of fields) {
    switch (field) {
      case 'title':
        out.title = values.title?.trim() ?? '';
        break;
      case 'subject_codes':
        out.subject_codes = values.subject_codes ?? [];
        break;
      case 'tags':
        out.tags = values.tags ?? [];
        break;
      case 'cohort':
        out.cohort = values.cohort ?? 0;
        break;
      case 'doc_type':
        out.doc_type = values.doc_type ?? '';
        break;
      case 'grade':
        out.grade = values.grade ?? '';
        break;
      case 'term':
        out.term = values.term ?? '';
        break;
      default:
        out[field] = (values[field] ?? '').trim();
    }
  }
  return out;
}

interface Props {
  form: UseFormReturnType<CatalogFormValues>;
  fields?: CatalogField[];
  facets?: PackFacets;
  /** 人工锁定的字段，表单项附注“已锁定”。 */
  locked?: string[];
  columns?: 1 | 2 | 3;
}

/** 表单校验：目前只有考试时间格式。 */
export function validateCatalog(values: CatalogFormValues): Record<string, string> {
  if (values.exam_date && !/^\d{4}-(0[1-9]|1[0-2])$/.test(values.exam_date)) return { exam_date: '格式为 YYYY-MM，如 2024-09' };
  return {};
}

export function PackCatalogFields({ form, fields = ALL_FIELDS, facets, locked = [], columns = 2 }: Props) {
  const note = (name: string) => (locked.includes(name) ? { description: '人工设定，AI 不会改动' } : {});
  const regionOptions = REGIONS.map((value) => ({ value, label: value }));
  const orgOptions = (facets?.organizations ?? []).map((f) => f.value);
  const tagOptions = (facets?.tags ?? []).map((f) => f.value);
  const has = (field: CatalogField) => fields.includes(field);
  const v = form.values;
  const set = <K extends keyof CatalogFormValues>(name: K) => (value: CatalogFormValues[K] | null) => form.setFieldValue(name, (value ?? undefined) as never);
  const err = (name: keyof CatalogFormValues) => form.errors[name as string];
  return (
    <SimpleGrid cols={{ base: 1, sm: columns }} spacing="md">
      {has('doc_type') ? <Select label={FIELD_LABELS.doc_type} {...note('doc_type')} clearable searchable data={DOC_TYPE_OPTIONS} placeholder="如 联考、月考、讲义" value={v.doc_type ?? null} onChange={(x) => set('doc_type')(x as DocType | null)} /> : null}
      {has('subject_codes') ? <MultiSelect label={FIELD_LABELS.subject_codes} {...note('subject_codes')} clearable searchable data={SUBJECT_OPTIONS} placeholder="选择学科" value={v.subject_codes ?? []} onChange={(x) => form.setFieldValue('subject_codes', x as SubjectCode[])} /> : null}
      {has('grade') ? <Select label={FIELD_LABELS.grade} {...note('grade')} clearable data={GRADE_OPTIONS} placeholder="选择年级" value={v.grade ?? null} onChange={(x) => set('grade')(x as Grade | null)} /> : null}
      {has('cohort') ? <Select label={FIELD_LABELS.cohort} {...note('cohort')} description={locked.includes('cohort') ? '人工设定，AI 不会改动' : '毕业年份，如 2025 届高三'} clearable data={cohortOptions().map((o) => ({ value: String(o.value), label: o.label }))} placeholder="选择届别" value={v.cohort ? String(v.cohort) : null} onChange={(x) => set('cohort')(x ? Number(x) : null)} /> : null}
      {has('term') ? <Select label={FIELD_LABELS.term} {...note('term')} clearable data={TERM_OPTIONS} placeholder="上 / 下学期" value={v.term ?? null} onChange={(x) => set('term')(x as Term | null)} /> : null}
      {has('region') ? <Select label={FIELD_LABELS.region} {...note('region')} clearable searchable data={regionOptions} placeholder="如 浙江、新高考I卷" value={v.region ?? null} onChange={set('region')} /> : null}
      {has('organization') ? <Autocomplete label={FIELD_LABELS.organization} {...note('organization')} data={orgOptions} placeholder="学校或联盟的规范全称" value={v.organization ?? ''} onChange={(x) => form.setFieldValue('organization', x || undefined)} /> : null}
      {has('exam_date') ? <TextInput label={FIELD_LABELS.exam_date} {...note('exam_date')} placeholder="YYYY-MM" maxLength={7} value={v.exam_date ?? ''} error={err('exam_date')} onChange={(e) => form.setFieldValue('exam_date', e.currentTarget.value || undefined)} /> : null}
      {has('exam_name') ? <TextInput label={FIELD_LABELS.exam_name} {...note('exam_name')} placeholder="试卷上印的考试名称" maxLength={255} value={v.exam_name ?? ''} onChange={(e) => form.setFieldValue('exam_name', e.currentTarget.value || undefined)} /> : null}
      {has('chapter') ? <TextInput label={FIELD_LABELS.chapter} {...note('chapter')} placeholder="教材章节或知识点" maxLength={255} value={v.chapter ?? ''} onChange={(e) => form.setFieldValue('chapter', e.currentTarget.value || undefined)} /> : null}
      {has('tags') ? <TagsInput label={FIELD_LABELS.tags} {...note('tags')} data={tagOptions} placeholder="回车添加" maxTags={30} clearable value={v.tags ?? []} onChange={(x) => form.setFieldValue('tags', x)} /> : null}
    </SimpleGrid>
  );
}
