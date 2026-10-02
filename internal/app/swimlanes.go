package app

import "context"

// Swimlanes lists the swimlanes of the project in board order, each with is_default telling
// whether it is the project's default_swimlane. The catalog entries are copied, not changed.
func (s *Service) Swimlanes(ctx context.Context) ([]Object, error) {
	items, err := s.Catalog(ctx, "swimlanes")
	if err != nil {
		return nil, err
	}
	def := ID(s.Project["default_swimlane"])
	out := make([]Object, 0, len(items))
	for _, item := range items {
		lane := Object{}
		for k, v := range item {
			lane[k] = v
		}
		lane["is_default"] = def > 0 && ID(item["id"]) == def
		out = append(out, lane)
	}
	SortStatuses(out)
	return out, nil
}
