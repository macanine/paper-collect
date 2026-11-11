package server

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/service"
	"github.com/nic/paper-collect/backend/internal/storage"
)

// The HTTP contract is generated from the DTOs below into
// web/src/shared/api/contract.gen.ts (`make contract`). A test fails when the
// checked-in file is stale, so the frontend cannot drift from the server.

// contractEnum is a closed vocabulary taken from Go constants.
type contractEnum struct {
	Const  string // exported array name, e.g. JOB_KINDS
	Type   string // union type name, e.g. JobKind
	Values []string
}

func subjectCodes() []string {
	out := make([]string, 0, len(model.Subjects))
	for _, s := range model.Subjects {
		out = append(out, s.Code)
	}
	return out
}

var contractEnums = []contractEnum{
	{"SUBJECT_CODES", "SubjectCode", subjectCodes()},
	{"PARTS", "Part", model.MaterialParts},
	{"DOC_TYPES", "DocType", model.DocTypes},
	{"GRADES", "Grade", model.Grades},
	{"TERMS", "Term", model.Terms},
	{"STAGES", "Stage", model.MaterialStages},
	{"AI_STEPS", "AIStep", []string{"", model.StepRead, model.StepGroup, model.StepDecide}},
	{"MATERIAL_STATUSES", "MaterialStatus", []string{model.StatusArchived, model.StatusMissing}},
	{"SYNC_STATUSES", "SyncStatus", model.SyncStatuses},
	{"USER_ROLES", "UserRole", []string{model.RoleAdmin, model.RoleEditor, model.RoleViewer}},
	{"PROVIDER_KINDS", "ProviderKind", model.StorageKinds},
	{"REPLICA_STATUSES", "ReplicaStatus", []string{model.ReplicaPending, model.ReplicaUploading, model.ReplicaMoving, model.ReplicaReady, model.ReplicaFailed, model.ReplicaSkipped, model.ReplicaOrphaned}},
	{"VERIFICATIONS", "Verification", []string{"uploaded", "size", "sha256"}},
	{"SHARE_STATUSES", "ShareStatus", []string{model.ShareStatusPending, model.ShareStatusReady, model.ShareStatusUnsupported, model.ShareStatusFailed}},
	{"JOB_KINDS", "JobKind", []string{model.JobIngest, model.JobReplicate, model.JobAnalyze, model.JobIndex, model.JobGroup, model.JobDecide, model.JobEntity}},
	{"JOB_STATUSES", "JobStatus", []string{model.JobPending, model.JobRunning, model.JobSucceeded, model.JobFailed}},
	{"ANALYSIS_STATUSES", "AnalysisStatus", []string{model.AnalysisPending, model.AnalysisRunning, model.AnalysisSucceeded, model.AnalysisFailed}},
	{"INDEX_STATUSES", "IndexStatus", []string{model.AnalysisPending, model.AnalysisRunning, model.AnalysisSucceeded, model.AnalysisFailed, model.IndexStatusNotApplicable}},
	{"UPLOAD_STATUSES", "UploadStatus", []string{model.UploadPending, model.UploadProcessing, model.UploadCompleted, model.UploadFailed}},
	{"UPLOAD_ENTRY_STATUSES", "UploadEntryStatus", []string{model.EntryPending, model.EntryReceived, model.EntryExisting}},
	{"SCOPES", "Scope", model.AllScopes},
	{"ON_PATH_CHANGE_MODES", "OnPathChange", model.OnPathChangeModes},
	{"DRIVE_ROUTE_MODES", "DriveRouteMode", model.DriveRouteModes},
	{"OCR_PROVIDER_KINDS", "OCRProviderKind", []string{model.OCRKindMinerU, model.OCRKindPaddleOCR, model.OCRKindRapidOCR}},
	{"ENTITY_KINDS", "EntityKind", []string{model.EntityOrganization, model.EntityTag}},
	{"ENTITY_SOURCES", "EntitySource", []string{model.EntitySourceAI, model.EntitySourceHuman}},
	{"NODE_STATUSES", "NodeStatus", []string{"online", "stale", "offline", "draining", "disabled"}},
	{"NODE_KINDS", "NodeKind", []string{model.JobReplicate, model.JobAnalyze, model.JobIndex}},
	{"SYNC_ACTION_KINDS", "SyncActionKind", []string{"upload", "move", "share", "skip", "orphan", "cancel", "drift", "unchanged", "not_required"}},
	{"JOB_LOG_LEVELS", "JobLogLevel", []string{"info", "error"}},
}

// contractType names one response shape.
type contractType struct {
	Name  string
	Value any
	Doc   string
}

var contractTypes = []contractType{
	{"User", userDTO{}, ""},
	{"AuthSession", loginDTO{}, ""},
	{"Subject", model.Subject{}, ""},
	{"APIKey", apiKeyDTO{}, ""},
	{"CreatedAPIKey", createKeyDTO{}, "The only response that contains the key secret."},
	{"Audit", auditDTO{}, ""},
	{"LibraryStats", libraryStatsDTO{}, ""},
	{"JobStats", jobStatsDTO{}, ""},
	{"PackBrief", packBriefDTO{}, ""},
	{"PackMember", packMemberDTO{}, ""},
	{"Pack", packDTO{}, ""},
	{"FacetValue", facetValueDTO{}, ""},
	{"PackFacets", packFacetsDTO{}, ""},
	{"Material", materialDTO{}, ""},
	{"MaterialSource", materialSourceDTO{}, ""},
	{"MaterialDetail", materialDetailDTO{}, ""},
	{"MaterialPullCache", pullCacheDTO{}, ""},
	{"InboxItem", reviewPackItemDTO{}, ""},
	{"InboxPack", reviewPackDTO{}, "One set in the inbox with the files that need attention."},
	{"Replica", replicaDTO{}, ""},
	{"Collection", collectionDTO{}, "A hand-curated list of packs."},
	{"CollectionDetail", collectionDetailDTO{}, ""},
	{"UploadEntry", uploadEntryDTO{}, ""},
	{"UploadHints", uploadHintsDTO{}, ""},
	{"UploadResult", uploadResultDTO{}, ""},
	{"Upload", uploadDTO{}, ""},
	{"FinalizeResult", finalizeDTO{}, ""},
	{"JobLog", model.JobLog{}, ""},
	{"Job", jobDTO{}, ""},
	{"DriveCapabilities", storage.Capabilities{}, ""},
	{"DriveNodeRoute", driveRouteDTO{}, ""},
	{"Drive", driveDTO{}, ""},
	{"DriveTestResult", driveTestDTO{}, ""},
	{"RuleConditions", model.RuleConditions{}, ""},
	{"SyncTarget", service.SyncTarget{}, ""},
	{"SyncRule", service.SyncRule{}, ""},
	{"SyncAction", service.SyncAction{}, ""},
	{"SyncPreview", service.SyncPreview{}, ""},
	{"ReplicaBulkResult", service.ReplicaBulkResult{}, ""},
	{"ReconcileResult", reconcileDTO{}, ""},
	{"CountResult", countDTO{}, ""},
	{"QueuedResult", queuedDTO{}, ""},
	{"AICard", catalogai.Card{}, "A verified model reading of one file."},
	{"AIProblem", catalogai.Problem{}, "One failed check; critical problems need a person."},
	{"AIEntityChoice", catalogai.EntityChoice{}, ""},
	{"AIFileDecision", catalogai.FileDecision{}, ""},
	{"AIDecision", catalogai.Decision{}, "A verified catalogue for one set."},
	{"AIProvider", aiProviderDTO{}, ""},
	{"AIProbeResult", probeDTO{}, ""},
	{"ProviderTestResult", providerTestDTO{}, ""},
	{"AISettings", aiSettingsDTO{}, ""},
	{"OCRProvider", ocrProviderDTO{}, ""},
	{"MaterialCard", materialCardDTO{}, ""},
	{"PackAI", packAIDTO{}, ""},
	{"Entity", entityDTO{}, "One canonical school/alliance or tag name."},
	{"RerunResult", rerunDTO{}, ""},
	{"SearchMaterialRef", searchMaterialDTO{}, ""},
	{"SearchHit", searchHitDTO{}, ""},
	{"SearchQuestionsResult", searchResultDTO{}, ""},
	{"SearchSettings", searchSettingsDTO{}, ""},
	{"QuestionRow", questionDTO{}, ""},
	{"MaterialIndexState", indexStateDTO{}, ""},
	{"IndexQueuedResult", indexQueuedDTO{}, ""},
	{"ClusterNode", nodeDTO{}, "An execution node; the master is one too."},
	{"CreatedNode", nodeCreatedDTO{}, "The only response that contains a node token."},
	{"ClusterSettings", clusterSettingsDTO{}, ""},
	{"NodePoint", service.NodePoint{}, ""},
}

// contractFields narrows string fields to their vocabulary, keyed by
// "GoType.json_name". The value is any TypeScript type expression.
var contractFields = map[string]string{
	"userDTO.role":                        "UserRole",
	"apiKeyDTO.scopes":                    "Scope[]",
	"packMemberDTO.part":                  "Part",
	"packMemberDTO.stage":                 "Stage",
	"packMemberDTO.status":                "MaterialStatus",
	"packMemberDTO.sync_status":           "SyncStatus",
	"packDTO.doc_type":                    "DocType | ''",
	"packDTO.subject_codes":               "SubjectCode[]",
	"packDTO.grade":                       "Grade",
	"packDTO.term":                        "Term",
	"packDTO.stage":                       "Stage",
	"materialDTO.part":                    "Part",
	"materialDTO.stage":                   "Stage",
	"materialDTO.ai_step":                 "AIStep",
	"materialDTO.status":                  "MaterialStatus",
	"materialDTO.sync_status":             "SyncStatus",
	"reviewPackItemDTO.stage":             "Stage",
	"replicaDTO.status":                   "ReplicaStatus",
	"replicaDTO.verification":             "Verification | ''",
	"replicaDTO.share_status":             "ShareStatus | ''",
	"uploadEntryDTO.status":               "UploadEntryStatus",
	"uploadHintsDTO.subject_codes":        "SubjectCode[]",
	"uploadHintsDTO.grade":                "Grade",
	"uploadHintsDTO.doc_type":             "DocType | ''",
	"uploadResultDTO.stage":               "Stage",
	"uploadResultDTO.part":                "Part",
	"uploadDTO.status":                    "UploadStatus",
	"JobLog.level":                        "JobLogLevel",
	"JobLog.metrics":                      "Record<string, string | number | boolean>",
	"jobDTO.kind":                         "JobKind",
	"jobDTO.status":                       "JobStatus",
	"driveDTO.kind":                       "ProviderKind",
	"driveDTO.on_path_change":             "OnPathChange",
	"driveDTO.route_mode":                 "DriveRouteMode",
	"driveRouteDTO.status":                "NodeStatus",
	"RuleConditions.subjects":             "SubjectCode[]",
	"RuleConditions.doc_types":            "DocType[]",
	"RuleConditions.grades":               "Grade[]",
	"RuleConditions.parts":                "Part[]",
	"SyncAction.action":                   "SyncActionKind",
	"SyncAction.status":                   "ReplicaStatus | ''",
	"SyncAction.share_status":             "ShareStatus | ''",
	"Card.role":                           "Part",
	"Card.doc_type":                       "DocType",
	"Card.subjects":                       "SubjectCode[]",
	"Card.grade":                          "Grade",
	"FileDecision.role":                   "Part",
	"Decision.doc_type":                   "DocType",
	"Decision.subjects":                   "SubjectCode[]",
	"Decision.grade":                      "Grade",
	"Decision.term":                       "Term",
	"ocrProviderDTO.kind":                 "OCRProviderKind",
	"materialCardDTO.status":              "'none' | AnalysisStatus",
	"packAIDTO.status":                    "'none' | AnalysisStatus",
	"entityDTO.kind":                      "EntityKind",
	"entityDTO.source":                    "EntitySource",
	"searchHitDTO.subject_codes":          "SubjectCode[]",
	"indexStateDTO.status":                "IndexStatus",
	"nodeDTO.status":                      "NodeStatus",
	"nodeDTO.kinds":                       "NodeKind[]",
	"nodeDTO.slots":                       "Partial<Record<NodeKind, number>>",
	"nodeDTO.configured_slots":            "Partial<Record<NodeKind, number>>",
	"nodeDTO.running_kinds":               "Partial<Record<JobKind, number>>",
	"clusterSettingsDTO.master_kinds":     "NodeKind[]",
	"materialCardDTO.problems":            "AIProblem[]",
	"libraryStatsDTO.needs_attention":     "number",
	"driveDTO.stats":                      "Record<string, number>",
	"packAIDTO.cards":                     "Record<string, AICard>",
	"searchHitDTO.highlights":             "[number, number][]",
	"materialDTO.attention_reasons":       "string[]",
	"reviewPackItemDTO.attention_reasons": "string[]",
}

var timeType = reflect.TypeOf(time.Time{})

type contractGen struct {
	names map[reflect.Type]string
	used  map[string]bool // override keys that matched a field
	buf   bytes.Buffer
}

// TypeScriptContract renders the generated contract file.
func TypeScriptContract() ([]byte, error) {
	g := &contractGen{names: map[reflect.Type]string{}, used: map[string]bool{}}
	for _, t := range contractTypes {
		rt := reflect.TypeOf(t.Value)
		if prev, ok := g.names[rt]; ok {
			return nil, fmt.Errorf("%s registered twice (also %s)", rt, prev)
		}
		g.names[rt] = t.Name
	}
	b := &g.buf
	b.WriteString("// Code generated by `make contract` from backend/internal/server/contract.go. DO NOT EDIT.\n")
	b.WriteString("// Snake_case field names are the wire format; do not rename them in the frontend.\n\n")
	for _, e := range contractEnums {
		fmt.Fprintf(b, "export const %s = [", e.Const)
		for i, v := range e.Values {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%q", v)
		}
		fmt.Fprintf(b, "] as const;\nexport type %s = (typeof %s)[number];\n\n", e.Type, e.Const)
	}
	b.WriteString("/** Scopes granted to each role; the server checks them again on every request. */\n")
	b.WriteString("export const ROLE_SCOPES: Record<UserRole, readonly Scope[]> = {\n")
	for _, role := range []string{model.RoleAdmin, model.RoleEditor, model.RoleViewer} {
		fmt.Fprintf(b, "  %s: [", role)
		for i, scope := range auth.RoleScopes(role) {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%q", scope)
		}
		b.WriteString("],\n")
	}
	b.WriteString("};\n\n")
	b.WriteString("export interface Page<T> {\n  items: T[];\n  total: number;\n  page: number;\n  page_size: number;\n}\n")
	for _, t := range contractTypes {
		rt := reflect.TypeOf(t.Value)
		if rt.Kind() != reflect.Struct {
			return nil, fmt.Errorf("%s is not a struct", t.Name)
		}
		b.WriteString("\n")
		if t.Doc != "" {
			fmt.Fprintf(b, "/** %s */\n", t.Doc)
		}
		var bases []string
		for i := range rt.NumField() {
			if f := rt.Field(i); f.Anonymous && f.Tag.Get("json") == "" {
				if name, ok := g.names[f.Type]; ok {
					bases = append(bases, name)
				}
			}
		}
		if len(bases) > 0 {
			fmt.Fprintf(b, "export interface %s extends %s {\n", t.Name, strings.Join(bases, ", "))
		} else {
			fmt.Fprintf(b, "export interface %s {\n", t.Name)
		}
		if err := g.fields(rt); err != nil {
			return nil, fmt.Errorf("%s: %w", t.Name, err)
		}
		b.WriteString("}\n")
	}
	var stale []string
	for key := range contractFields {
		if !g.used[key] {
			stale = append(stale, key)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		return nil, fmt.Errorf("contract field overrides match no field: %s", strings.Join(stale, ", "))
	}
	return b.Bytes(), nil
}

func (g *contractGen) fields(rt reflect.Type) error {
	for i := range rt.NumField() {
		f := rt.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" || (!f.IsExported() && !f.Anonymous) {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			if _, ok := g.names[f.Type]; ok {
				continue // emitted as `extends`
			}
			if err := g.fields(f.Type); err != nil {
				return err
			}
			continue
		}
		if name == "" {
			return fmt.Errorf("field %s has no json tag", f.Name)
		}
		key := rt.Name() + "." + name
		ts, ok := contractFields[key]
		if ok {
			g.used[key] = true
		} else {
			var err error
			if ts, err = g.typeOf(f.Type); err != nil {
				return fmt.Errorf("field %s: %w", f.Name, err)
			}
		}
		optional := strings.Contains(opts, "omitempty")
		if f.Type.Kind() == reflect.Pointer && !optional {
			ts += " | null"
		}
		mark := ""
		if optional {
			mark = "?"
		}
		fmt.Fprintf(&g.buf, "  %s%s: %s;\n", name, mark, ts)
	}
	return nil
}

func (g *contractGen) typeOf(t reflect.Type) (string, error) {
	if t == timeType {
		return "string", nil
	}
	if name, ok := g.names[t]; ok {
		return name, nil
	}
	switch t.Kind() {
	case reflect.Pointer:
		return g.typeOf(t.Elem())
	case reflect.String:
		return "string", nil
	case reflect.Bool:
		return "boolean", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number", nil
	case reflect.Slice, reflect.Array:
		elem, err := g.typeOf(t.Elem())
		if err != nil {
			return "", err
		}
		if t.Kind() == reflect.Array {
			parts := make([]string, t.Len())
			for i := range parts {
				parts[i] = elem
			}
			return "[" + strings.Join(parts, ", ") + "]", nil
		}
		if strings.ContainsAny(elem, " |") {
			elem = "(" + elem + ")"
		}
		return elem + "[]", nil
	case reflect.Map:
		value, err := g.typeOf(t.Elem())
		if err != nil {
			return "", err
		}
		return "Record<string, " + value + ">", nil
	case reflect.Interface:
		return "unknown", nil
	case reflect.Struct:
		return "", fmt.Errorf("struct %s is not registered in contractTypes", t)
	}
	return "", fmt.Errorf("unsupported kind %s", t.Kind())
}
