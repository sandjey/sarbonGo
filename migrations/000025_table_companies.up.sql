--
-- PostgreSQL database dump
--

\restrict bqXyVIHnKnS7pyAQ6ul6ytyB8aOAYTbRLpGUvfttkHb9exqbJSBA8cfIdbwIuGE

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
-- Name: companies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.companies (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    name character varying NOT NULL,
    inn character varying,
    address character varying,
    phone character varying,
    email character varying,
    website character varying,
    license_number character varying,
    status character varying DEFAULT 'pending'::character varying NOT NULL,
    rating double precision,
    completed_orders integer DEFAULT 0 NOT NULL,
    cancelled_orders integer DEFAULT 0 NOT NULL,
    total_revenue numeric(18,2) DEFAULT 0 NOT NULL,
    created_by uuid,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    deleted_at timestamp without time zone,
    max_cargo integer DEFAULT 0 NOT NULL,
    max_dispatchers integer DEFAULT 0 NOT NULL,
    max_managers integer DEFAULT 0 NOT NULL,
    max_top_dispatchers integer DEFAULT 0 NOT NULL,
    max_top_managers integer DEFAULT 0 NOT NULL,
    max_vehicles integer DEFAULT 0 NOT NULL,
    max_drivers integer DEFAULT 0 NOT NULL,
    owner_id uuid,
    company_type character varying(20),
    auto_approve_limit numeric(10,2),
    owner_dispatcher_id uuid,
    CONSTRAINT companies_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'inactive'::character varying, 'blocked'::character varying, 'pending'::character varying])::text[]))),
    CONSTRAINT companies_type_check CHECK (((company_type IS NULL) OR ((company_type)::text = ''::text) OR ((company_type)::text = ANY ((ARRAY['CargoOwner'::character varying, 'Carrier'::character varying, 'Expeditor'::character varying])::text[]))))
);


--
-- Name: companies companies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.companies
    ADD CONSTRAINT companies_pkey PRIMARY KEY (id);


--
-- Name: idx_companies_created_by; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_companies_created_by ON public.companies USING btree (created_by);


--
-- Name: idx_companies_inn_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_companies_inn_unique ON public.companies USING btree (inn) WHERE ((inn IS NOT NULL) AND ((inn)::text <> ''::text));


--
-- Name: idx_companies_license_number_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_companies_license_number_unique ON public.companies USING btree (license_number) WHERE ((license_number IS NOT NULL) AND ((license_number)::text <> ''::text));


--
-- Name: idx_companies_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_companies_name ON public.companies USING btree (name);


--
-- Name: idx_companies_owner_dispatcher_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_companies_owner_dispatcher_id ON public.companies USING btree (owner_dispatcher_id) WHERE (owner_dispatcher_id IS NOT NULL);


--
-- Name: idx_companies_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_companies_status ON public.companies USING btree (status);


--
-- Name: companies fk_companies_created_by_admins; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.companies
    ADD CONSTRAINT fk_companies_created_by_admins FOREIGN KEY (created_by) REFERENCES public.admins(id);


--
-- Name: companies fk_companies_owner_app_users; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.companies
    ADD CONSTRAINT fk_companies_owner_app_users FOREIGN KEY (owner_id) REFERENCES public.company_users(id);


--
-- Name: companies fk_companies_owner_dispatcher; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.companies
    ADD CONSTRAINT fk_companies_owner_dispatcher FOREIGN KEY (owner_dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE SET NULL;


--
-- PostgreSQL database dump complete
--

\unrestrict bqXyVIHnKnS7pyAQ6ul6ytyB8aOAYTbRLpGUvfttkHb9exqbJSBA8cfIdbwIuGE

