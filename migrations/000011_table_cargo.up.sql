--
-- PostgreSQL database dump
--

\restrict f7rXuhwobbHObgWcEbTA2VopSAKRy0m3LTxhMYBVzT8ExT4OvQMxZn3L1OYeqxY

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: cargo; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    weight double precision NOT NULL,
    volume double precision NOT NULL,
    ready_enabled boolean DEFAULT false NOT NULL,
    ready_at timestamp without time zone,
    load_comment character varying,
    truck_type character varying NOT NULL,
    temp_min double precision,
    temp_max double precision,
    adr_enabled boolean DEFAULT false NOT NULL,
    adr_class character varying,
    loading_types text[],
    requirements text[],
    shipment_type character varying,
    belts_count integer,
    documents jsonb,
    contact_name character varying,
    contact_phone character varying,
    status character varying DEFAULT 'created'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    deleted_at timestamp without time zone,
    created_by_type character varying,
    created_by_id uuid,
    company_id uuid,
    moderation_rejection_reason text,
    name character varying(255),
    cargo_type_id uuid,
    capacity_required double precision,
    packaging character varying(500),
    dimensions character varying(500),
    photo_urls text[],
    power_plate_type character varying,
    trailer_plate_type character varying,
    vehicles_amount integer,
    vehicles_left integer NOT NULL,
    way_points jsonb,
    packaging_amount integer,
    is_two_drivers_required boolean DEFAULT false NOT NULL,
    unloading_types text[],
    prev_status text,
    CONSTRAINT cargo_created_by_type_check CHECK (((created_by_type IS NULL) OR ((created_by_type)::text = ANY ((ARRAY['ADMIN'::character varying, 'DISPATCHER'::character varying, 'COMPANY'::character varying])::text[])))),
    CONSTRAINT cargo_prev_status_check CHECK (((prev_status IS NULL) OR (prev_status = ANY (ARRAY['SEARCHING_ALL'::text, 'SEARCHING_COMPANY'::text])))),
    CONSTRAINT cargo_status_check CHECK (((status)::text = ANY ((ARRAY['PENDING_MODERATION'::character varying, 'SEARCHING_ALL'::character varying, 'SEARCHING_COMPANY'::character varying, 'PROCESSING'::character varying, 'COMPLETED'::character varying, 'CANCELLED'::character varying])::text[]))),
    CONSTRAINT cargo_weight_check CHECK ((weight > (0)::double precision))
);


--
-- Name: cargo cargo_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo
    ADD CONSTRAINT cargo_pkey PRIMARY KEY (id);


--
-- Name: idx_cargo_company_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_company_id ON public.cargo USING btree (company_id) WHERE (company_id IS NOT NULL);


--
-- Name: idx_cargo_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_created_at ON public.cargo USING btree (created_at);


--
-- Name: idx_cargo_created_by_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_created_by_id ON public.cargo USING btree (created_by_id) WHERE (created_by_id IS NOT NULL);


--
-- Name: idx_cargo_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_deleted_at ON public.cargo USING btree (deleted_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_cargo_power_plate_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_power_plate_type ON public.cargo USING btree (power_plate_type);


--
-- Name: idx_cargo_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_status ON public.cargo USING btree (status);


--
-- Name: idx_cargo_trailer_plate_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_trailer_plate_type ON public.cargo USING btree (trailer_plate_type);


--
-- Name: idx_cargo_truck_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_truck_type ON public.cargo USING btree (truck_type);


--
-- Name: idx_cargo_vehicles_amount; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_vehicles_amount ON public.cargo USING btree (vehicles_amount);


--
-- Name: idx_cargo_vehicles_left; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_vehicles_left ON public.cargo USING btree (vehicles_left);


--
-- Name: idx_cargo_weight; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_weight ON public.cargo USING btree (weight);


--
-- Name: cargo fk_cargo_cargo_type; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo
    ADD CONSTRAINT fk_cargo_cargo_type FOREIGN KEY (cargo_type_id) REFERENCES public.cargo_types(id);


--
-- Name: cargo fk_cargo_company_id; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo
    ADD CONSTRAINT fk_cargo_company_id FOREIGN KEY (company_id) REFERENCES public.companies(id);


--
-- PostgreSQL database dump complete
--

\unrestrict f7rXuhwobbHObgWcEbTA2VopSAKRy0m3LTxhMYBVzT8ExT4OvQMxZn3L1OYeqxY

