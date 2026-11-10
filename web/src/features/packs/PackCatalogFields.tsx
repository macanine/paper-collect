import { AutoComplete, Form, Input, Select } from 'antd';
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
  fields?: CatalogField[];
  facets?: PackFacets;
  /** 人工锁定的字段，表单项附注“已锁定”。 */
  locked?: string[];
  columns?: 1 | 2 | 3;
}

export function PackCatalogFields({ fields = ALL_FIELDS, facets, locked = [], columns = 2 }: Props) {
  const warn = (name: string) => (locked.includes(name) ? { help: '人工设定，AI 不会改动' } : {});
  const regionOptions = REGIONS.map((value) => ({ value, label: value }));
  const orgOptions = (facets?.organizations ?? []).map((f) => ({ value: f.value }));
  const tagOptions = (facets?.tags ?? []).map((f) => ({ value: f.value, label: f.value }));
  const has = (field: CatalogField) => fields.includes(field);
  return (
    <div className={`catalog-grid catalog-grid--${columns}`}>
      {has('doc_type') ? (
        <Form.Item name="doc_type" label={FIELD_LABELS.doc_type} {...warn('doc_type')}>
          <Select allowClear showSearch optionFilterProp="label" options={DOC_TYPE_OPTIONS} placeholder="如 联考、月考、讲义" />
        </Form.Item>
      ) : null}
      {has('subject_codes') ? (
        <Form.Item name="subject_codes" label={FIELD_LABELS.subject_codes} {...warn('subject_codes')}>
          <Select mode="multiple" allowClear optionFilterProp="label" options={SUBJECT_OPTIONS} placeholder="选择学科" />
        </Form.Item>
      ) : null}
      {has('grade') ? (
        <Form.Item name="grade" label={FIELD_LABELS.grade} {...warn('grade')}>
          <Select allowClear options={GRADE_OPTIONS} placeholder="选择年级" />
        </Form.Item>
      ) : null}
      {has('cohort') ? (
        <Form.Item name="cohort" label={FIELD_LABELS.cohort} {...warn('cohort')} tooltip="毕业年份，如 2025 届高三">
          <Select allowClear options={cohortOptions()} placeholder="选择届别" />
        </Form.Item>
      ) : null}
      {has('term') ? (
        <Form.Item name="term" label={FIELD_LABELS.term} {...warn('term')}>
          <Select allowClear options={TERM_OPTIONS} placeholder="上 / 下学期" />
        </Form.Item>
      ) : null}
      {has('region') ? (
        <Form.Item name="region" label={FIELD_LABELS.region} {...warn('region')}>
          <Select allowClear showSearch optionFilterProp="label" options={regionOptions} placeholder="如 浙江、新高考I卷" />
        </Form.Item>
      ) : null}
      {has('organization') ? (
        <Form.Item name="organization" label={FIELD_LABELS.organization} {...warn('organization')}>
          <AutoComplete allowClear options={orgOptions} placeholder="学校或联盟的规范全称" filterOption />
        </Form.Item>
      ) : null}
      {has('exam_date') ? (
        <Form.Item
          name="exam_date"
          label={FIELD_LABELS.exam_date}
          {...warn('exam_date')}
          rules={[{ pattern: /^\d{4}-(0[1-9]|1[0-2])$/, message: '格式为 YYYY-MM，如 2024-09' }]}
        >
          <Input allowClear placeholder="YYYY-MM" maxLength={7} />
        </Form.Item>
      ) : null}
      {has('exam_name') ? (
        <Form.Item name="exam_name" label={FIELD_LABELS.exam_name} {...warn('exam_name')}>
          <Input allowClear placeholder="试卷上印的考试名称" maxLength={255} />
        </Form.Item>
      ) : null}
      {has('chapter') ? (
        <Form.Item name="chapter" label={FIELD_LABELS.chapter} {...warn('chapter')}>
          <Input allowClear placeholder="教材章节或知识点" maxLength={255} />
        </Form.Item>
      ) : null}
      {has('tags') ? (
        <Form.Item name="tags" label={FIELD_LABELS.tags} {...warn('tags')}>
          <Select mode="tags" allowClear options={tagOptions} placeholder="回车添加" maxCount={30} />
        </Form.Item>
      ) : null}
    </div>
  );
}
